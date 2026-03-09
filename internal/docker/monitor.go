package docker

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"

	"github.com/HarkushaVlad/docker-monitor-bot/internal/notification"
	"github.com/HarkushaVlad/docker-monitor-bot/internal/utils"
)

func (s *Service) MonitorEvents(ctx context.Context, chatID int64, notifier notification.Notifier) {
	eventCh, errCh := s.client.Events(ctx, types.EventsOptions{})

	for {
		select {
		case event := <-eventCh:
			if event.Type != events.ContainerEventType {
				continue
			}
			s.handleContainerEvent(event, chatID, notifier)
		case err := <-errCh:
			if err != nil {
				log.Printf("Docker events error: %v", err)
				time.Sleep(10 * time.Second)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) handleContainerEvent(event events.Message, chatID int64, notifier notification.Notifier) {
	shortID := event.ID[:12]
	name := event.Actor.Attributes["name"]

	project := event.Actor.Attributes[LabelComposeProject]
	service := event.Actor.Attributes[LabelComposeService]

	origin := name
	if project != "" && service != "" {
		origin = fmt.Sprintf("%s / %s", project, service)
	}

	switch event.Status {
	case "start":
		msg := fmt.Sprintf(
			"🚀 <b>Container started</b>\n\n"+
				"<pre>┌ Name: %s\n"+
				"└ ID:   %s</pre>",
			origin, shortID,
		)
		log.Printf("Container started: %s (%s)", origin, shortID)
		notifier.SendText(chatID, msg)

	case "die", "oom":
		msg := fmt.Sprintf(
			"❗ <b>Container stopped</b>\n\n"+
				"<pre>┌ Name:   %s\n"+
				"├ ID:     %s\n"+
				"└ Reason: %s</pre>",
			origin, shortID, event.Status,
		)
		log.Printf("Container stopped: %s (%s) reason=%s", origin, shortID, event.Status)
		notifier.SendText(chatID, msg)
	}
}

func (s *Service) MonitorLogs(ctx context.Context, pollInterval time.Duration, tailCount int, chatID int64, notifier notification.Notifier) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	errorRegex := regexp.MustCompile(`(?i)error`)
	lastMarkers := make(map[string]string)

	for {
		select {
		case <-ticker.C:
			s.checkContainerLogs(ctx, tailCount, chatID, notifier, errorRegex, lastMarkers)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) checkContainerLogs(ctx context.Context, tailCount int, chatID int64, notifier notification.Notifier, errorRegex *regexp.Regexp, lastMarkers map[string]string) {
	containers, err := s.client.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		log.Printf("Error fetching containers: %v", err)
		return
	}

	for _, c := range containers {
		if c.State != "running" {
			continue
		}
		go s.scanContainerLogs(ctx, c, tailCount, chatID, notifier, errorRegex, lastMarkers)
	}
}

func (s *Service) scanContainerLogs(ctx context.Context, c types.Container, tailCount int, chatID int64, notifier notification.Notifier, errorRegex *regexp.Regexp, lastMarkers map[string]string) {
	name := utils.ContainerName(c)

	out, err := s.client.ContainerLogs(ctx, c.ID, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       fmt.Sprintf("%d", tailCount),
	})
	if err != nil {
		log.Printf("Error fetching logs for %s: %v", name, err)
		return
	}
	defer out.Close()

	scanner := bufio.NewScanner(out)
	var lines []string
	var hashes []string
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
		hashes = append(hashes, utils.HashString(line))
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Error scanning logs for %s: %v", name, err)
		return
	}
	if len(lines) == 0 {
		return
	}

	startIdx := 0
	if marker, ok := lastMarkers[c.ID]; ok && marker != "" {
		for i, h := range hashes {
			if h == marker {
				startIdx = i + 1
				break
			}
		}
	}

	if startIdx >= len(lines) {
		lastMarkers[c.ID] = hashes[len(hashes)-1]
		return
	}

	var errors []string
	for _, line := range lines[startIdx:] {
		if errorRegex.MatchString(line) {
			errors = append(errors, line)
		}
	}

	if len(errors) > 0 {
		maxErrors := min(3, len(errors))
		var formatted []string
		for _, errLine := range errors[:maxErrors] {
			clean := utils.RemoveControlChars(strings.ToValidUTF8(errLine, ""))
			formatted = append(formatted, fmt.Sprintf("<pre>%s</pre>", utils.EscapeHTML(clean)))
		}

		project := c.Labels[LabelComposeProject]
		service := c.Labels[LabelComposeService]
		displayName := name
		if project != "" && service != "" {
			displayName = fmt.Sprintf("%s / %s", project, service)
		}

		msg := fmt.Sprintf(
			"🚨 <b>Errors in <u>%s</u>:</b>\n\n%s",
			displayName,
			strings.Join(formatted, "\n"),
		)
		log.Printf("Errors in %s:\n%s", name, strings.Join(errors, "\n"))
		notifier.SendText(chatID, msg)
	}

	lastMarkers[c.ID] = hashes[len(hashes)-1]
}

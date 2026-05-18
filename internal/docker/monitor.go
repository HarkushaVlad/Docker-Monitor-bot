package docker

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"

	"github.com/HarkushaVlad/docker-monitor-bot/internal/notification"
	"github.com/HarkushaVlad/docker-monitor-bot/internal/utils"
)

func (s *Service) MonitorEvents(ctx context.Context, chatIDs []int64, notifier notification.Notifier) {
	eventCh, errCh := s.client.Events(ctx, types.EventsOptions{})

	for {
		select {
		case event := <-eventCh:
			if event.Type != events.ContainerEventType {
				continue
			}
			s.handleContainerEvent(event, chatIDs, notifier)
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

func (s *Service) handleContainerEvent(event events.Message, chatIDs []int64, notifier notification.Notifier) {
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
		for _, chatID := range chatIDs {
			notifier.SendText(chatID, msg)
		}

	case "die", "oom":
		msg := fmt.Sprintf(
			"❗ <b>Container stopped</b>\n\n"+
				"<pre>┌ Name:   %s\n"+
				"├ ID:     %s\n"+
				"└ Reason: %s</pre>",
			origin, shortID, event.Status,
		)
		log.Printf("Container stopped: %s (%s) reason=%s", origin, shortID, event.Status)
		for _, chatID := range chatIDs {
			notifier.SendText(chatID, msg)
		}
	}
}

type logTracker struct {
	mu          sync.Mutex
	lastChecked map[string]time.Time
}

var (
	errorRegex        = regexp.MustCompile(`(?i)\berror\b`)
	continuationRegex = regexp.MustCompile(`(?i)^\s+|^at\s+|^caused by|^\t`)
)

func (s *Service) MonitorLogs(ctx context.Context, pollInterval time.Duration, chatIDs []int64, notifier notification.Notifier) {
	tracker := &logTracker{
		lastChecked: make(map[string]time.Time),
	}

	s.initLogTracker(ctx, tracker)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkContainerLogs(ctx, chatIDs, notifier, tracker)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) initLogTracker(ctx context.Context, tracker *logTracker) {
	containers, err := s.client.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		log.Printf("Error initializing log tracker: %v", err)
		return
	}

	now := time.Now()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	for _, c := range containers {
		if c.State == "running" {
			tracker.lastChecked[c.ID] = now
		}
	}
}

func (s *Service) checkContainerLogs(ctx context.Context, chatIDs []int64, notifier notification.Notifier, tracker *logTracker) {
	containers, err := s.client.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		log.Printf("Error fetching containers: %v", err)
		return
	}

	for _, c := range containers {
		if c.State != "running" {
			continue
		}
		s.scanContainerLogs(ctx, c, chatIDs, notifier, tracker)
	}
}

func (s *Service) scanContainerLogs(ctx context.Context, c types.Container, chatIDs []int64, notifier notification.Notifier, tracker *logTracker) {
	tracker.mu.Lock()
	since, exists := tracker.lastChecked[c.ID]
	if !exists {
		tracker.lastChecked[c.ID] = time.Now()
		tracker.mu.Unlock()
		return
	}
	tracker.mu.Unlock()

	now := time.Now()

	out, err := s.client.ContainerLogs(ctx, c.ID, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Since:      since.Format(time.RFC3339Nano),
	})
	if err != nil {
		log.Printf("Error fetching logs for %s: %v", utils.ContainerName(c), err)
		return
	}
	defer out.Close()

	scanner := bufio.NewScanner(out)
	var lines []string
	for scanner.Scan() {
		raw := utils.StripDockerLogHeader(scanner.Bytes())
		line := string(raw)
		line = utils.RemoveControlChars(strings.ToValidUTF8(line, ""))
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Error scanning logs for %s: %v", utils.ContainerName(c), err)
	}

	tracker.mu.Lock()
	tracker.lastChecked[c.ID] = now
	tracker.mu.Unlock()

	if len(lines) == 0 {
		return
	}

	groups := groupErrors(lines)
	if len(groups) == 0 {
		return
	}

	name := utils.ContainerName(c)
	project := c.Labels[LabelComposeProject]
	service := c.Labels[LabelComposeService]
	displayName := name
	if project != "" && service != "" {
		displayName = fmt.Sprintf("%s / %s", project, service)
	}

	logCtx := LogContext{
		ContainerName: name,
		ProjectName:   project,
		ServiceName:   service,
		DisplayName:   displayName,
	}

	filteredGroups := make([][]string, 0, len(groups))
	for _, group := range groups {
		if s.ignoreStore != nil && s.ignoreStore.ShouldIgnore(logCtx, group) {
			continue
		}
		filteredGroups = append(filteredGroups, group)
	}
	if len(filteredGroups) == 0 {
		return
	}
	groups = filteredGroups

	if len(groups) > 3 {
		groups = groups[:3]
	}

	var formatted []string
	for _, group := range groups {
		escaped := utils.EscapeHTML(strings.Join(group, "\n"))
		formatted = append(formatted, fmt.Sprintf("<pre>%s</pre>", escaped))
	}

	msg := fmt.Sprintf(
		"🚨 <b>Errors in <u>%s</u>:</b>\n\n%s",
		displayName,
		strings.Join(formatted, "\n"),
	)
	log.Printf("Errors in %s: %d group(s)", name, len(groups))
	for _, chatID := range chatIDs {
		notifier.SendText(chatID, msg)
	}
}

func groupErrors(lines []string) [][]string {
	var groups [][]string
	var current []string
	inGroup := false

	for _, line := range lines {
		isError := errorRegex.MatchString(line)
		isContinuation := inGroup && continuationRegex.MatchString(line)

		if isError {
			if len(current) > 0 {
				groups = append(groups, current)
			}
			current = []string{line}
			inGroup = true
		} else if isContinuation && len(current) < 10 {
			current = append(current, line)
		} else {
			if len(current) > 0 {
				groups = append(groups, current)
				current = nil
			}
			inGroup = false
		}
	}

	if len(current) > 0 {
		groups = append(groups, current)
	}

	return groups
}

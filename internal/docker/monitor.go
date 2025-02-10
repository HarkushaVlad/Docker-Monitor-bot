package docker

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/HarkushaVlad/docker-monitor-bot/internal/notification"
	"github.com/HarkushaVlad/docker-monitor-bot/internal/utils"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
)

func MonitorDockerEvents(ctx context.Context, telegramChatID int64, notifier notification.Notifier) {
	options := types.EventsOptions{}
	eventCh, errCh := DockerClient.Events(ctx, options)

	for {
		select {
		case event := <-eventCh:
			if event.Type == events.ContainerEventType {
				if event.Status == "start" {
					message := fmt.Sprintf(
						"🚀 <b>Container started</b>\n\n"+
							"<pre>"+
							"┌ ID: %s\n"+
							"└ Name: %s"+
							"</pre>",
						event.ID[:12],
						event.Actor.Attributes["name"],
					)
					log.Printf("Container started: ID=%s, Name=%s", event.ID[:12], event.Actor.Attributes["name"])
					notifier.SendText(telegramChatID, message)
				}
				if event.Status == "die" || event.Status == "oom" {
					message := fmt.Sprintf(
						"❗️ <b>Container stopped</b>\n\n"+
							"<pre>"+
							"┌ ID: %s\n"+
							"├ Name: %s\n"+
							"└ Status: %s"+
							"</pre>",
						event.ID[:12],
						event.Actor.Attributes["name"],
						event.Status,
					)
					log.Printf("Container stopped: ID=%s, Name=%s, Status=%s", event.ID[:12], event.Actor.Attributes["name"], event.Status)
					notifier.SendText(telegramChatID, message)
				}
			}
		case err := <-errCh:
			if err != nil {
				log.Printf("Error receiving Docker events: %v", err)
				time.Sleep(10 * time.Second)
			}
		}
	}
}

func MonitorContainerLogs(ctx context.Context, pollInterval time.Duration, tailCount int, telegramChatID int64, notifier notification.Notifier) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	errorRegex := regexp.MustCompile(`(?i)error`)
	lastMarkers := make(map[string]string)

	for {
		select {
		case <-ticker.C:
			containers, err := DockerClient.ContainerList(ctx, types.ContainerListOptions{All: true})
			if err != nil {
				log.Printf("Error fetching container list: %v", err)
				continue
			}

			for _, container := range containers {
				go func(c types.Container) {
					options := types.ContainerLogsOptions{
						ShowStdout: true,
						ShowStderr: true,
						Tail:       fmt.Sprintf("%d", tailCount),
					}

					out, err := DockerClient.ContainerLogs(ctx, c.ID, options)
					if err != nil {
						log.Printf("Error fetching logs for container %s: %v", strings.TrimPrefix(c.Names[0], "/"), err)
						return
					}
					defer out.Close()

					scanner := bufio.NewScanner(out)
					var lines []string
					var lineHashes []string
					for scanner.Scan() {
						line := scanner.Text()
						lines = append(lines, line)
						lineHashes = append(lineHashes, utils.HashString(line))
					}
					if err := scanner.Err(); err != nil {
						log.Printf("Error scanning logs for container %s: %v", strings.TrimPrefix(c.Names[0], "/"), err)
						return
					}

					storedMarker, exists := lastMarkers[c.ID]
					startIndex := 0
					if exists && storedMarker != "" {
						found := false
						for i, h := range lineHashes {
							if h == storedMarker {
								startIndex = i + 1
								found = true
								break
							}
						}
						if !found {
							startIndex = 0
						}
					}

					if startIndex < len(lines) {
						newLines := lines[startIndex:]
						var errors []string
						for _, line := range newLines {
							if errorRegex.MatchString(line) {
								errors = append(errors, line)
							}
						}
						if len(errors) > 0 {
							var errorMessages []string
							for _, errLine := range errors[:utils.Min(3, len(errors))] {
								filteredString := utils.RemoveControlCharactersRegex(strings.ToValidUTF8(errLine, ""))
								escapedString := utils.EscapeHTML(filteredString)
								errorMessages = append(errorMessages, fmt.Sprintf("<pre>%s</pre>", escapedString))
							}

							message := fmt.Sprintf(
								"🚨 <b>Container <u>%s</u> encountered errors:</b>\n\n%s",
								strings.TrimPrefix(c.Names[0], "/"),
								errorMessages,
							)
							log.Printf("Errors detected in container %s:\n%s",
								strings.TrimPrefix(c.Names[0], "/"),
								strings.Join(errors, "\n"),
							)
							notifier.SendText(telegramChatID, message)
						}
						lastMarkers[c.ID] = lineHashes[len(lineHashes)-1]
					}
				}(container)
			}
		case <-ctx.Done():
			return
		}
	}
}

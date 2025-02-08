package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

//go:embed locales/*.json
var localeFiles embed.FS

var (
	telegramBot    *tgbotapi.BotAPI
	telegramChatID int64
	dockerClient   *client.Client
	pollInterval   time.Duration
	tailCount      string
	translations   map[string]string
)

func loadLocalization(lang string) error {
	filePath := fmt.Sprintf("locales/%s.json", lang)
	data, err := localeFiles.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("error loading localization file: %v", err)
	}
	err = json.Unmarshal(data, &translations)
	if err != nil {
		return fmt.Errorf("error parsing localization file: %v", err)
	}
	return nil
}

func loadConfig() error {
	err := godotenv.Load()
	if err != nil {
		return fmt.Errorf("error loading .env file: %v", err)
	}

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN not set")
	}

	chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
	if chatIDStr == "" {
		return fmt.Errorf("TELEGRAM_CHAT_ID not set")
	}
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid TELEGRAM_CHAT_ID format: %v", err)
	}
	telegramChatID = chatID

	intervalStr := os.Getenv("POLL_INTERVAL_SECONDS")
	if intervalStr == "" {
		pollInterval = 60 * time.Second
	} else {
		seconds, err := strconv.Atoi(intervalStr)
		if err != nil {
			pollInterval = 60 * time.Second
		} else {
			pollInterval = time.Duration(seconds) * time.Second
		}
	}

	tailCountStr := os.Getenv("TAIL_COUNT")
	tailCountNum, err := strconv.Atoi(tailCountStr)
	if err != nil || tailCountNum <= 0 {
		tailCount = "100"
	}

	lang := os.Getenv("LANGUAGE")
	if lang == "" {
		lang = "en"
	}

	err = loadLocalization(lang)
	if err != nil {
		return err
	}

	return nil
}

func initTelegramBot() error {
	var err error
	telegramBot, err = tgbotapi.NewBotAPI(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if err != nil {
		return fmt.Errorf("failed to initialize Telegram bot: %v", err)
	}
	telegramBot.Debug = false
	log.Printf("Logged in as: %s", telegramBot.Self.UserName)
	return nil
}

func initDockerClient() error {
	var err error
	dockerClient, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to initialize Docker client: %v", err)
	}
	return nil
}

func sendTelegramNotification(message string) {
	validString := strings.ToValidUTF8(message, "")
	msg := tgbotapi.NewMessage(telegramChatID, validString)
	msg.ParseMode = tgbotapi.ModeHTML
	_, err := telegramBot.Send(msg)
	if err != nil {
		log.Printf("Error sending Telegram notification: %v", err)
	} else {
		log.Println("Telegram notification sent")
	}
}

func monitorDockerEvents(ctx context.Context) {
	options := types.EventsOptions{}
	eventCh, errCh := dockerClient.Events(ctx, options)

	for {
		select {
		case event := <-eventCh:
			if event.Type == events.ContainerEventType {
				if event.Status == "start" {
					message := fmt.Sprintf(
						translations["docker_container_started"],
						event.ID[:12],
						event.Actor.Attributes["name"],
					)
					log.Printf("Container started: ID=%s, Name=%s",
						event.ID[:12], event.Actor.Attributes["name"])
					sendTelegramNotification(message)
				}

				if event.Status == "die" || event.Status == "oom" {
					message := fmt.Sprintf(
						translations["docker_container_stopped"],
						event.ID[:12],
						event.Actor.Attributes["name"],
						event.Status,
					)
					log.Printf("Container stopped: ID=%s, Name=%s, Status=%s",
						event.ID[:12], event.Actor.Attributes["name"], event.Status)
					sendTelegramNotification(message)
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

func monitorContainerLogs(ctx context.Context) {
	tailCountOption := tailCount
	lastMarkers := make(map[string]string)
	errorRegex := regexp.MustCompile(`(?i)error`)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			containers, err := dockerClient.ContainerList(ctx, types.ContainerListOptions{All: true})
			if err != nil {
				log.Printf("Error fetching container list: %v", err)
				continue
			}

			for _, container := range containers {
				go func(c types.Container) {
					options := types.ContainerLogsOptions{
						ShowStdout: true,
						ShowStderr: true,
						Tail:       tailCountOption,
					}

					out, err := dockerClient.ContainerLogs(ctx, c.ID, options)
					if err != nil {
						log.Printf("Error fetching logs for container %s: %v", strings.TrimPrefix(c.Names[0], "/"), err)
						return
					}
					defer func() {
						if err := out.Close(); err != nil {
							log.Printf("Error closing log stream for container %s: %v", strings.TrimPrefix(c.Names[0], "/"), err)
						}
					}()

					scanner := bufio.NewScanner(out)
					var lines []string
					var lineHashes []string
					for scanner.Scan() {
						line := scanner.Text()
						lines = append(lines, line)
						lineHashes = append(lineHashes, hashString(line))
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
							for _, errLine := range errors[:min(3, len(errors))] {
								filteredString := removeControlCharactersRegex(strings.ToValidUTF8(errLine, ""))
								errorMessages = append(errorMessages, fmt.Sprintf("<pre>%s</pre>", filteredString))
							}

							message := fmt.Sprintf(
								translations["docker_container_errors"],
								strings.TrimPrefix(c.Names[0], "/"),
								strings.Join(errorMessages, ""),
							)

							log.Printf("Errors detected in container %s:\n%s",
								strings.TrimPrefix(c.Names[0], "/"),
								strings.Join(errors, "\n"),
							)

							sendTelegramNotification(message)
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

func handleCheckCommand(update tgbotapi.Update) {
	if update.Message.Chat.ID != telegramChatID {
		return
	}

	ctx := context.Background()
	containers, err := dockerClient.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		sendTelegramNotification(fmt.Sprintf(translations["error_fetching_containers"], err))
		return
	}

	if len(containers) == 0 {
		sendTelegramNotification(translations["no_containers_running"])
		return
	}

	var runningContainers []types.Container
	var stoppedContainers []types.Container

	for _, container := range containers {
		if container.State == "running" {
			runningContainers = append(runningContainers, container)
		} else {
			stoppedContainers = append(stoppedContainers, container)
		}
	}

	var statusLines []string
	statusLines = append(statusLines, translations["containers_status"])

	for _, container := range runningContainers {
		statusLines = append(statusLines, fmt.Sprintf(
			"<pre>┌ ID: %s\n├ Name: %s\n├ Status: 🟢 Running\n├ Image: %s\n└ Started: %s</pre>",
			container.ID[:12],
			strings.TrimPrefix(container.Names[0], "/"),
			container.Image,
			time.Unix(container.Created, 0).Format("2006-01-02 15:04:05"),
		))
	}

	for _, container := range stoppedContainers {
		statusLines = append(statusLines, fmt.Sprintf(
			"<pre>┌ ID: %s\n├ Name: %s\n├ Status: 🔴 Stopped\n└ Image: %s\n└ Started: %s</pre>",
			container.ID[:12],
			strings.TrimPrefix(container.Names[0], "/"),
			container.Image,
			time.Unix(container.Created, 0).Format("2006-01-02 15:04:05"),
		))
	}

	reply := strings.Join(statusLines, "")
	sendTelegramNotification(reply)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func removeControlCharactersRegex(s string) string {
	// [\x00-\x08], [\x0B-\x0C], and [\x0E-\x1F]
	re := regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F]`)
	return re.ReplaceAllString(s, "")
}

func hashString(s string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return strconv.FormatUint(h.Sum64(), 16)
}

func main() {
	if err := loadConfig(); err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	if err := initTelegramBot(); err != nil {
		log.Fatalf("Failed to initialize Telegram bot: %v", err)
	}

	if err := initDockerClient(); err != nil {
		log.Fatalf("Failed to initialize Docker client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go monitorDockerEvents(ctx)
	go monitorContainerLogs(ctx)

	updates := telegramBot.GetUpdatesChan(tgbotapi.UpdateConfig{Timeout: 60})

	go func() {
		for update := range updates {
			if update.Message != nil && update.Message.IsCommand() && update.Message.Chat.ID == telegramChatID {
				switch update.Message.Command() {
				case "check":
					handleCheckCommand(update)
				}
			}
		}
	}()

	log.Println("Docker monitoring bot started...")
	select {}
}

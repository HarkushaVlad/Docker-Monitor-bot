package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/HarkushaVlad/docker-monitor-bot/internal/docker"
	"github.com/HarkushaVlad/docker-monitor-bot/internal/notification"
)

const itemsPerPage = 6

var currentPage int = 0

var shortIDMap = make(map[string]string)

func HandleCallbacks(bot *tgbotapi.BotAPI, notifier notification.Notifier) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		if update.CallbackQuery != nil {
			HandleCallbackQuery(bot, update.CallbackQuery, notifier)
		} else if update.Message != nil && update.Message.IsCommand() {
			HandleCommand(bot, update.Message, notifier)
		}
	}
}

func HandleCommand(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, notifier notification.Notifier) {
	chatID := msg.Chat.ID

	switch msg.Command() {
	case "check":
		HandleCheckCommand(chatID, notifier)
	case "list":
		currentPage = 0
		showContainerList(chatID, currentPage, notifier)
	}
}

func HandleCallbackQuery(bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, notifier notification.Notifier) {
	chatID := query.Message.Chat.ID
	msgID := query.Message.MessageID
	data := query.Data

	bot.Request(tgbotapi.NewDeleteMessage(chatID, msgID))

	switch {
	case strings.HasPrefix(data, "container_"):
		containerID := strings.TrimPrefix(data, "container_")
		showContainerDetails(chatID, containerID, notifier)
	case strings.HasPrefix(data, "page_"):
		handlePageNavigation(chatID, data, notifier)
	case strings.HasPrefix(data, "action_"):
		handleContainerAction(chatID, data, notifier)
	}
}

func HandleCheckCommand(chatID int64, notifier notification.Notifier) {
	ctx := context.Background()
	containers, err := docker.DockerClient.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		notifier.SendText(chatID, fmt.Sprintf("Error retrieving container list: %v", err))
		return
	}

	if len(containers) == 0 {
		notifier.SendText(chatID, "🔍 <b>No containers are running</b>")
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
	statusLines = append(statusLines, "📊 <b>Containers Status:</b>\n\n")

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
	notifier.SendText(chatID, reply)
}

func showContainerList(chatID int64, page int, notifier notification.Notifier) {
	ctx := context.Background()
	containers, err := docker.DockerClient.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		sendErrorMessage(chatID, "Failed to fetch containers", notifier)
		return
	}

	totalPages := (len(containers)-1)/itemsPerPage + 1
	start := page * itemsPerPage
	end := start + itemsPerPage
	if end > len(containers) {
		end = len(containers)
	}

	var buttons []tgbotapi.InlineKeyboardButton
	for _, container := range containers[start:end] {
		shortID := container.ID[:12]
		shortIDMap[shortID] = container.ID

		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s %s", getStatusIcon(container.State), getContainerName(container)),
			fmt.Sprintf("container_%s", shortID),
		)
		buttons = append(buttons, btn)
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 0; i < len(buttons); i += 2 {
		if i+1 < len(buttons) {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(buttons[i], buttons[i+1]))
		} else {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(buttons[i]))
		}
	}

	var paginationRow []tgbotapi.InlineKeyboardButton
	if page > 0 {
		paginationRow = append(paginationRow, tgbotapi.NewInlineKeyboardButtonData("⬅", "page_prev"))
	}
	if page < totalPages-1 {
		paginationRow = append(paginationRow, tgbotapi.NewInlineKeyboardButtonData("➡", "page_next"))
	}
	if len(paginationRow) > 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(paginationRow...))
	}

	keyboard := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}

	msgText := fmt.Sprintf("📦 Containers (%d-%d of %d):", start+1, end, len(containers))
	notifier.SendTextWithKeyboard(chatID, msgText, keyboard)
}

func showContainerDetails(chatID int64, shortID string, notifier notification.Notifier) {
	fullID, exists := shortIDMap[shortID]
	if !exists {
		sendErrorMessage(chatID, "Container not found", notifier)
		return
	}

	ctx := context.Background()
	container, err := docker.DockerClient.ContainerInspect(ctx, fullID)
	if err != nil {
		sendErrorMessage(chatID, "Container not found", notifier)
		return
	}

	status := "🔴 Stopped"
	if container.State.Running {
		status = "🟢 Running"
	}

	createdTime, err := time.Parse(time.RFC3339Nano, container.Created)
	if err != nil {
		log.Printf("Error parsing creation time: %v", err)
		createdTime = time.Now()
	}

	text := fmt.Sprintf(
		"<pre>"+
			"┌ Name: %s\n"+
			"├ Status: %s\n"+
			"├ Image: %s\n"+
			"└ Created: %s"+
			"</pre>",
		strings.TrimPrefix(container.Name, "/"),
		status,
		container.Config.Image,
		createdTime.Format("2006-01-02 15:04:05"),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("▶️ Start", fmt.Sprintf("action_start_%s", shortID)),
			tgbotapi.NewInlineKeyboardButtonData("⏹️ Stop", fmt.Sprintf("action_stop_%s", shortID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Restart", fmt.Sprintf("action_restart_%s", shortID)),
			tgbotapi.NewInlineKeyboardButtonData("↩️ Back", "page_back"),
		),
	)

	notifier.SendTextWithKeyboard(chatID, text, keyboard)
}

func handlePageNavigation(chatID int64, action string, notifier notification.Notifier) {
	switch action {
	case "page_prev":
		if currentPage > 0 {
			currentPage--
		}
	case "page_next":
		currentPage++
	case "page_back":
		currentPage = 0
	}
	showContainerList(chatID, currentPage, notifier)
}

func handleContainerAction(chatID int64, action string, notifier notification.Notifier) {
	parts := strings.Split(action, "_")
	if len(parts) < 3 {
		return
	}
	shortID := parts[2]

	fullID, exists := shortIDMap[shortID]
	if !exists {
		sendErrorMessage(chatID, "Container not found", notifier)
		return
	}

	actionType := parts[1]
	ctx := context.Background()
	var err error

	switch actionType {
	case "start":
		err = docker.DockerClient.ContainerStart(ctx, fullID, types.ContainerStartOptions{})
	case "stop":
		timeout := 10 * time.Second
		err = docker.DockerClient.ContainerStop(ctx, fullID, &timeout)
	case "restart":
		timeout := 10 * time.Second
		err = docker.DockerClient.ContainerRestart(ctx, fullID, &timeout)
	}

	if err != nil {
		sendErrorMessage(chatID, fmt.Sprintf("Failed to %s container: %v", actionType, err), notifier)
		return
	}

	msgText := fmt.Sprintf("✅ Command <i>'%s'</i> for container <u><b>%s</b></u> executed successfully", actionType, shortID)
	notifier.SendText(chatID, msgText)
	showContainerDetails(chatID, shortID, notifier)
}

func getContainerName(container types.Container) string {
	return strings.TrimPrefix(container.Names[0], "/")
}

func getStatusIcon(state string) string {
	if state == "running" {
		return "🟢"
	}
	return "🔴"
}

func sendErrorMessage(chatID int64, message string, notifier notification.Notifier) {
	notifier.SendText(chatID, "❌ "+message)
}

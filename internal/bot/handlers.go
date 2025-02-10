package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/HarkushaVlad/docker-monitor-bot/internal/docker"
	"github.com/HarkushaVlad/docker-monitor-bot/internal/notification"
)

const itemsPerPage = 6

type BotState struct {
	LastMessageID int
	CurrentPage   int
	ShortIDMap    map[string]string
}

var (
	states   = make(map[int64]*BotState)
	stateMux = &sync.Mutex{}
)

func getState(chatID int64) *BotState {
	stateMux.Lock()
	defer stateMux.Unlock()

	if _, ok := states[chatID]; !ok {
		states[chatID] = &BotState{
			ShortIDMap: make(map[string]string),
		}
	}
	return states[chatID]
}

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
	state := getState(chatID)

	switch msg.Command() {
	case "check":
		HandleCheckCommand(chatID, notifier, state)
	case "list":
		if state.LastMessageID != 0 {
			notifier.DeleteMessage(chatID, state.LastMessageID)
		}
		state.LastMessageID = 0
		state.CurrentPage = 0
		showContainerList(chatID, state, notifier)
	}
}

func HandleCallbackQuery(bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, notifier notification.Notifier) {
	chatID := query.Message.Chat.ID
	msgID := query.Message.MessageID
	data := query.Data
	state := getState(chatID)

	notifier.AnswerCallbackQuery(query.ID, "")

	switch {
	case strings.HasPrefix(data, "container_"):
		shortID := strings.TrimPrefix(data, "container_")
		showContainerDetails(chatID, msgID, shortID, notifier, state)
	case strings.HasPrefix(data, "page_"):
		handlePageNavigation(chatID, data, notifier, state)
	case strings.HasPrefix(data, "action_"):
		handleContainerAction(chatID, msgID, data, notifier, state)
	}
}

func HandleCheckCommand(chatID int64, notifier notification.Notifier, state *BotState) {
	ctx := context.Background()
	containers, err := docker.DockerClient.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		editOrSendMessage(chatID, state.LastMessageID, fmt.Sprintf("Error retrieving container list: %v", err), notifier)
		return
	}

	if len(containers) == 0 {
		editOrSendMessage(chatID, state.LastMessageID, "🔍 <b>No containers are running</b>", notifier)
		return
	}

	var statusLines []string
	statusLines = append(statusLines, "📊 <b>Containers Status:</b>\n\n")

	for _, container := range containers {
		statusLines = append(statusLines, formatContainerInfo(container))
	}

	reply := strings.Join(statusLines, "")
	editOrSendMessage(chatID, state.LastMessageID, reply, notifier)
}

func showContainerList(chatID int64, state *BotState, notifier notification.Notifier) {
	ctx := context.Background()
	containers, err := docker.DockerClient.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		editOrSendErrorMessage(chatID, state.LastMessageID, "Failed to fetch containers", notifier)
		return
	}

	totalPages := (len(containers)-1)/itemsPerPage + 1
	start := state.CurrentPage * itemsPerPage
	end := start + itemsPerPage
	if end > len(containers) {
		end = len(containers)
	}

	state.ShortIDMap = make(map[string]string)
	var buttons []tgbotapi.InlineKeyboardButton
	for _, container := range containers[start:end] {
		shortID := container.ID[:12]
		state.ShortIDMap[shortID] = container.ID

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
	if state.CurrentPage > 0 {
		paginationRow = append(paginationRow, tgbotapi.NewInlineKeyboardButtonData("⬅", "page_prev"))
	}
	if state.CurrentPage < totalPages-1 {
		paginationRow = append(paginationRow, tgbotapi.NewInlineKeyboardButtonData("➡", "page_next"))
	}
	if len(paginationRow) > 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(paginationRow...))
	}

	keyboard := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	msgText := fmt.Sprintf("📦 Containers (%d-%d of %d):", start+1, end, len(containers))

	if state.LastMessageID == 0 {
		state.LastMessageID = notifier.SendTextWithKeyboard(chatID, msgText, keyboard)
	} else {
		notifier.EditMessageWithKeyboard(chatID, state.LastMessageID, msgText, keyboard)
	}
}

func showContainerDetails(chatID int64, messageID int, shortID string, notifier notification.Notifier, state *BotState) {
	fullID, exists := state.ShortIDMap[shortID]
	if !exists {
		editOrSendErrorMessage(chatID, messageID, "Container not found", notifier)
		return
	}

	ctx := context.Background()
	container, err := docker.DockerClient.ContainerInspect(ctx, fullID)
	if err != nil {
		editOrSendErrorMessage(chatID, messageID, "Container not found", notifier)
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

	notifier.EditMessageWithKeyboard(chatID, messageID, text, keyboard)
	state.LastMessageID = messageID
}

func handlePageNavigation(chatID int64, action string, notifier notification.Notifier, state *BotState) {
	switch strings.TrimPrefix(action, "page_") {
	case "prev":
		if state.CurrentPage > 0 {
			state.CurrentPage--
		}
	case "next":
		state.CurrentPage++
	case "back":
		state.CurrentPage = 0
	}

	showContainerList(chatID, state, notifier)
}

func handleContainerAction(chatID int64, messageID int, action string, notifier notification.Notifier, state *BotState) {
	parts := strings.Split(action, "_")
	if len(parts) < 3 {
		return
	}
	actionType := parts[1]
	shortID := parts[2]

	fullID, exists := state.ShortIDMap[shortID]
	if !exists {
		editOrSendErrorMessage(chatID, messageID, "Container not found", notifier)
		return
	}

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
		editOrSendErrorMessage(chatID, messageID, fmt.Sprintf("Failed to %s container: %v", actionType, err), notifier)
		return
	}

	msgText := fmt.Sprintf("✅ Command <i>'%s'</i> for container <u><b>%s</b></u> executed successfully", actionType, shortID)
	notifier.EditMessageText(chatID, messageID, msgText)
	showContainerDetails(chatID, messageID, shortID, notifier, state)
}

func formatContainerInfo(container types.Container) string {
	createdTime := time.Unix(container.Created, 0)
	return fmt.Sprintf(
		"<pre>┌ ID: %s\n├ Name: %s\n├ Status: %s\n├ Image: %s\n└ Started: %s</pre>",
		container.ID[:12],
		getContainerName(container),
		getStatusIcon(container.State),
		container.Image,
		createdTime.Format("2006-01-02 15:04:05"),
	)
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

func editOrSendMessage(chatID int64, messageID int, text string, notifier notification.Notifier) {
	if messageID > 0 {
		notifier.EditMessageText(chatID, messageID, text)
	} else {
		notifier.SendText(chatID, text)
	}
}

func editOrSendErrorMessage(chatID int64, messageID int, text string, notifier notification.Notifier) {
	editOrSendMessage(chatID, messageID, "❌ "+text, notifier)
}

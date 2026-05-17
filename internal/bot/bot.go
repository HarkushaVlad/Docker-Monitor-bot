package bot

import (
	"fmt"
	"log"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/HarkushaVlad/docker-monitor-bot/internal/docker"
	"github.com/HarkushaVlad/docker-monitor-bot/internal/notification"
)

type Bot struct {
	api      *tgbotapi.BotAPI
	Docker   *docker.Service
	ChatIDs  []int64
	notifier notification.Notifier
	states   map[int64]*State
	mu       sync.Mutex
}

func New(token string, dockerSvc *docker.Service, chatIDs []int64) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to init telegram bot: %v", err)
	}
	api.Debug = false
	log.Printf("Telegram bot: @%s", api.Self.UserName)

	b := &Bot{
		api:     api,
		Docker:  dockerSvc,
		ChatIDs: chatIDs,
		states:  make(map[int64]*State),
	}
	b.notifier = &telegramNotifier{api: api}
	return b, nil
}

func (b *Bot) Notifier() notification.Notifier {
	return b.notifier
}

func (b *Bot) Run() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		switch {
		case update.CallbackQuery != nil:
			if b.IsAllowed(update.CallbackQuery.Message.Chat.ID) {
				b.handleCallback(update.CallbackQuery)
			}
		case update.Message != nil && update.Message.IsCommand():
			if b.IsAllowed(update.Message.Chat.ID) {
				b.notifier.DeleteMessage(update.Message.Chat.ID, update.Message.MessageID)
				b.handleCommand(update.Message)
			}
		}
	}
}

func (b *Bot) IsAllowed(chatID int64) bool {
	for _, id := range b.ChatIDs {
		if id == chatID {
			return true
		}
	}
	return false
}

func (b *Bot) getState(chatID int64) *State {
	b.mu.Lock()
	defer b.mu.Unlock()

	if s, ok := b.states[chatID]; ok {
		return s
	}
	s := &State{
		View:       viewMain,
		ShortIDMap: make(map[string]string),
		ProjectMap: make(map[int]string),
	}
	b.states[chatID] = s
	return s
}

type telegramNotifier struct {
	api *tgbotapi.BotAPI
}

func (n *telegramNotifier) SendText(chatID int64, message string) int {
	msg := tgbotapi.NewMessage(chatID, strings.ToValidUTF8(message, ""))
	msg.ParseMode = tgbotapi.ModeHTML
	sent, err := n.api.Send(msg)
	if err != nil {
		log.Printf("Error sending message: %v", err)
		return 0
	}
	return sent.MessageID
}

func (n *telegramNotifier) SendTextWithKeyboard(chatID int64, message string, keyboard tgbotapi.InlineKeyboardMarkup) int {
	msg := tgbotapi.NewMessage(chatID, strings.ToValidUTF8(message, ""))
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = keyboard
	sent, err := n.api.Send(msg)
	if err != nil {
		log.Printf("Error sending message: %v", err)
		return 0
	}
	return sent.MessageID
}

func (n *telegramNotifier) EditMessageText(chatID int64, messageID int, text string) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, strings.ToValidUTF8(text, ""))
	edit.ParseMode = tgbotapi.ModeHTML
	if _, err := n.api.Send(edit); err != nil {
		log.Printf("Error editing message: %v", err)
	}
}

func (n *telegramNotifier) EditMessageWithKeyboard(chatID int64, messageID int, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, strings.ToValidUTF8(text, ""), keyboard)
	edit.ParseMode = tgbotapi.ModeHTML
	if _, err := n.api.Send(edit); err != nil {
		log.Printf("Error editing message: %v", err)
	}
}

func (n *telegramNotifier) AnswerCallbackQuery(callbackID string, text string) {
	cb := tgbotapi.NewCallback(callbackID, text)
	if _, err := n.api.Request(cb); err != nil {
		log.Printf("Error answering callback: %v", err)
	}
}

func (n *telegramNotifier) DeleteMessage(chatID int64, messageID int) {
	del := tgbotapi.NewDeleteMessage(chatID, messageID)
	if _, err := n.api.Request(del); err != nil {
		log.Printf("Error deleting message: %v", err)
	}
}

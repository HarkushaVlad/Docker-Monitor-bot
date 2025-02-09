package notification

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Notifier interface {
	SendText(chatID int64, message string)
	SendTextWithKeyboard(chatID int64, message string, keyboard tgbotapi.InlineKeyboardMarkup)
}

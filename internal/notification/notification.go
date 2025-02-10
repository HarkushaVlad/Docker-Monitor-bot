package notification

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Notifier interface {
	SendText(chatID int64, message string) int
	SendTextWithKeyboard(chatID int64, message string, keyboard tgbotapi.InlineKeyboardMarkup) int
	EditMessageText(chatID int64, messageID int, text string)
	EditMessageWithKeyboard(chatID int64, messageID int, text string, keyboard tgbotapi.InlineKeyboardMarkup)
	AnswerCallbackQuery(callbackID string, text string)
	DeleteMessage(chatID int64, messageID int)
}

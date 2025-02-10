package bot

import (
	"fmt"
	"log"
	"os"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var TelegramBot *tgbotapi.BotAPI

func InitTelegramBot() error {
	var err error
	TelegramBot, err = tgbotapi.NewBotAPI(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if err != nil {
		return fmt.Errorf("failed to initialize Telegram bot: %v", err)
	}
	TelegramBot.Debug = false
	log.Printf("Logged in as: %s", TelegramBot.Self.UserName)
	return nil
}

type TelegramNotifier struct {
	Bot *tgbotapi.BotAPI
}

func (n *TelegramNotifier) SendText(chatID int64, message string) int {
	validMessage := strings.ToValidUTF8(message, "")
	msg := tgbotapi.NewMessage(chatID, validMessage)
	msg.ParseMode = tgbotapi.ModeHTML
	sentMsg, err := n.Bot.Send(msg)
	if err != nil {
		log.Printf("Error sending message: %v", err)
		return 0
	}
	return sentMsg.MessageID
}

func (n *TelegramNotifier) SendTextWithKeyboard(chatID int64, message string, keyboard tgbotapi.InlineKeyboardMarkup) int {
	validMessage := strings.ToValidUTF8(message, "")
	msg := tgbotapi.NewMessage(chatID, validMessage)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = keyboard
	sentMsg, err := n.Bot.Send(msg)
	if err != nil {
		log.Printf("Error sending message with keyboard: %v", err)
		return 0
	}
	return sentMsg.MessageID
}

func (n *TelegramNotifier) EditMessageText(chatID int64, messageID int, text string) {
	validText := strings.ToValidUTF8(text, "")
	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, validText)
	editMsg.ParseMode = tgbotapi.ModeHTML
	_, err := n.Bot.Send(editMsg)
	if err != nil {
		log.Printf("Error editing message: %v", err)
	}
}

func (n *TelegramNotifier) EditMessageWithKeyboard(chatID int64, messageID int, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	validText := strings.ToValidUTF8(text, "")
	editMsg := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, validText, keyboard)
	editMsg.ParseMode = tgbotapi.ModeHTML
	_, err := n.Bot.Send(editMsg)
	if err != nil {
		log.Printf("Error editing message with keyboard: %v", err)
	}
}

func (n *TelegramNotifier) AnswerCallbackQuery(callbackID string, text string) {
	answer := tgbotapi.NewCallback(callbackID, text)
	if _, err := n.Bot.Request(answer); err != nil {
		log.Printf("Error answering callback: %v", err)
	}
}

func (n *TelegramNotifier) DeleteMessage(chatID int64, messageID int) {
	deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
	if _, err := n.Bot.Request(deleteMsg); err != nil {
		log.Printf("Error deleting message: %v", err)
	}
}

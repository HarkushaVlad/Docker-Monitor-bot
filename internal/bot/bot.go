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

func (tn *TelegramNotifier) SendText(chatID int64, message string) {
	validMessage := strings.ToValidUTF8(message, "")
	msg := tgbotapi.NewMessage(chatID, validMessage)
	msg.ParseMode = tgbotapi.ModeHTML
	_, err := tn.Bot.Send(msg)
	if err != nil {
		log.Printf("Error sending Telegram notification: %v", err)
	} else {
		log.Println("Telegram notification sent")
	}
}

func (tn *TelegramNotifier) SendTextWithKeyboard(chatID int64, message string, keyboard tgbotapi.InlineKeyboardMarkup) {
	validMessage := strings.ToValidUTF8(message, "")
	msg := tgbotapi.NewMessage(chatID, validMessage)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = keyboard
	_, err := tn.Bot.Send(msg)
	if err != nil {
		log.Printf("Error sending Telegram notification with keyboard: %v", err)
	} else {
		log.Println("Telegram notification with keyboard sent")
	}
}

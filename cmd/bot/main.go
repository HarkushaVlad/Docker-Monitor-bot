package main

import (
	"context"
	"log"

	"github.com/HarkushaVlad/docker-monitor-bot/internal/bot"
	"github.com/HarkushaVlad/docker-monitor-bot/internal/config"
	"github.com/HarkushaVlad/docker-monitor-bot/internal/docker"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	if err := bot.InitTelegramBot(); err != nil {
		log.Fatalf("Failed to initialize Telegram bot: %v", err)
	}

	notifier := &bot.TelegramNotifier{Bot: bot.TelegramBot}

	if err := docker.InitDockerClient(); err != nil {
		log.Fatalf("Failed to initialize Docker client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go docker.MonitorDockerEvents(ctx, cfg.TelegramChatID, notifier)

	go docker.MonitorContainerLogs(ctx, cfg.PollInterval, cfg.TailCount, cfg.TelegramChatID, notifier)

	go func() {
		u := tgbotapi.NewUpdate(0)
		u.Timeout = 60
		updates := bot.TelegramBot.GetUpdatesChan(u)

		for update := range updates {
			if update.CallbackQuery != nil {
				bot.HandleCallbackQuery(bot.TelegramBot, update.CallbackQuery, notifier)
			}
			if update.Message != nil && update.Message.IsCommand() && update.Message.Chat.ID == cfg.TelegramChatID {
				bot.HandleCommand(bot.TelegramBot, update.Message, notifier)
			}
		}
	}()

	log.Println("Docker monitoring bot started...")
	select {}
}

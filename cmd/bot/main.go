package main

import (
	"context"
	"log"

	"github.com/HarkushaVlad/docker-monitor-bot/internal/bot"
	"github.com/HarkushaVlad/docker-monitor-bot/internal/config"
	"github.com/HarkushaVlad/docker-monitor-bot/internal/docker"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	dockerSvc, err := docker.NewService(cfg.LogIgnoreRulesFile)
	if err != nil {
		log.Fatalf("Failed to init Docker client: %v", err)
	}

	b, err := bot.New(cfg.TelegramBotToken, dockerSvc, cfg.TelegramChatIDs)
	if err != nil {
		log.Fatalf("Failed to init Telegram bot: %v", err)
	}

	if err := b.RegisterCommands(); err != nil {
		log.Printf("Warning: failed to register bot commands: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	notifier := b.Notifier()
	go dockerSvc.MonitorEvents(ctx, cfg.TelegramChatIDs, notifier)
	go dockerSvc.MonitorLogs(ctx, cfg.PollInterval, cfg.TelegramChatIDs, notifier)

	log.Println("Docker monitoring bot started")
	b.Run()
}

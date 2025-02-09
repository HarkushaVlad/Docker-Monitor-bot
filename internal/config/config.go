package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramBotToken string
	TelegramChatID   int64
	DockerHost       string
	PollInterval     time.Duration
	TailCount        int
	Language         string
}

func LoadConfig() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf("error loading .env file: %v", err)
	}

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN not set")
	}

	chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
	if chatIDStr == "" {
		return nil, fmt.Errorf("TELEGRAM_CHAT_ID not set")
	}
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid TELEGRAM_CHAT_ID format: %v", err)
	}

	pollIntervalStr := os.Getenv("POLL_INTERVAL_SECONDS")
	var pollInterval time.Duration
	if pollIntervalStr == "" {
		pollInterval = 60 * time.Second
	} else {
		seconds, err := strconv.Atoi(pollIntervalStr)
		if err != nil {
			pollInterval = 60 * time.Second
		} else {
			pollInterval = time.Duration(seconds) * time.Second
		}
	}

	tailCountStr := os.Getenv("TAIL_COUNT")
	tailCount, err := strconv.Atoi(tailCountStr)
	if err != nil || tailCount <= 0 {
		tailCount = 100
	}

	lang := os.Getenv("LANGUAGE")
	if lang == "" {
		lang = "en"
	}

	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		dockerHost = "unix:///var/run/docker.sock"
	}

	return &Config{
		TelegramBotToken: botToken,
		TelegramChatID:   chatID,
		DockerHost:       dockerHost,
		PollInterval:     pollInterval,
		TailCount:        tailCount,
		Language:         lang,
	}, nil
}

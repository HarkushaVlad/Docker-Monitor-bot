package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramBotToken   string
	TelegramChatIDs    []int64
	DockerHost         string
	PollInterval       time.Duration
	LogIgnoreRulesFile string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
	if chatIDStr == "" {
		return nil, fmt.Errorf("TELEGRAM_CHAT_ID is required")
	}

	var chatIDs []int64
	for _, idStr := range strings.Split(chatIDStr, ",") {
		idStr = strings.TrimSpace(idStr)
		if idStr == "" {
			continue
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid TELEGRAM_CHAT_ID '%s': %v", idStr, err)
		}
		chatIDs = append(chatIDs, id)
	}

	if len(chatIDs) == 0 {
		return nil, fmt.Errorf("at least one TELEGRAM_CHAT_ID is required")
	}

	pollInterval := 60 * time.Second
	if s := os.Getenv("POLL_INTERVAL_SECONDS"); s != "" {
		if sec, err := strconv.Atoi(s); err == nil && sec > 0 {
			pollInterval = time.Duration(sec) * time.Second
		}
	}

	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		dockerHost = "unix:///var/run/docker.sock"
	}

	logIgnoreRulesFile := os.Getenv("LOG_IGNORE_RULES_FILE")
	if logIgnoreRulesFile == "" {
		logIgnoreRulesFile = "log-ignore-rules.json"
	}

	return &Config{
		TelegramBotToken:   token,
		TelegramChatIDs:    chatIDs,
		DockerHost:         dockerHost,
		PollInterval:       pollInterval,
		LogIgnoreRulesFile: logIgnoreRulesFile,
	}, nil
}

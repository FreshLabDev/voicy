// SPDX-License-Identifier: Apache-2.0
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	TelegramBotToken    string
	DeepgramAPIKey      string
	DatabaseURL         string
	MigrationsDir       string
	AutoMigrate         bool
	HTTPAddr            string
	LogLevel            string
	TranscriptRetention time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		TelegramBotToken: strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		DeepgramAPIKey:   strings.TrimSpace(os.Getenv("DEEPGRAM_API_KEY")),
		DatabaseURL:      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		MigrationsDir:    valueOrDefault("MIGRATIONS_DIR", "./migrations"),
		HTTPAddr:         valueOrDefault("HTTP_ADDR", ":8080"),
		LogLevel:         valueOrDefault("LOG_LEVEL", "info"),
	}
	var err error
	if cfg.AutoMigrate, err = strconv.ParseBool(valueOrDefault("AUTO_MIGRATE", "true")); err != nil {
		return Config{}, fmt.Errorf("AUTO_MIGRATE must be a boolean: %w", err)
	}
	if cfg.TranscriptRetention, err = time.ParseDuration(valueOrDefault("TRANSCRIPT_RETENTION", "2160h")); err != nil || cfg.TranscriptRetention <= 0 {
		return Config{}, fmt.Errorf("TRANSCRIPT_RETENTION must be a positive duration")
	}
	if cfg.TelegramBotToken == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.DeepgramAPIKey == "" {
		return Config{}, fmt.Errorf("DEEPGRAM_API_KEY is required")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func valueOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

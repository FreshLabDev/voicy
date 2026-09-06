// SPDX-License-Identifier: Apache-2.0
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultTelegramAPIBase is Telegram's own server. A self-hosted one is set
// through TELEGRAM_API_BASE.
const DefaultTelegramAPIBase = "https://api.telegram.org"

type Config struct {
	TelegramBotToken    string
	TelegramAPIBase     string
	DeepgramAPIKey      string
	DatabaseURL         string
	MigrationsDir       string
	AutoMigrate         bool
	HTTPAddr            string
	LogLevel            string
	TranscriptRetention time.Duration
	MaxMediaBytes       int64
	MaxMediaDuration    time.Duration
	JobStaleAfter       time.Duration
	MaxConcurrentJobs   int
	StatsTimezone       string
	StatsCacheTTL       time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		TelegramBotToken: strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		DeepgramAPIKey:   strings.TrimSpace(os.Getenv("DEEPGRAM_API_KEY")),
		DatabaseURL:      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		TelegramAPIBase:  strings.TrimRight(valueOrDefault("TELEGRAM_API_BASE", DefaultTelegramAPIBase), "/"),
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
	if cfg.MaxMediaBytes, err = strconv.ParseInt(valueOrDefault("MAX_MEDIA_BYTES", "20971520"), 10, 64); err != nil || cfg.MaxMediaBytes <= 0 {
		return Config{}, fmt.Errorf("MAX_MEDIA_BYTES must be a positive integer")
	}
	if cfg.MaxMediaDuration, err = time.ParseDuration(valueOrDefault("MAX_MEDIA_DURATION", "1h")); err != nil || cfg.MaxMediaDuration <= 0 {
		return Config{}, fmt.Errorf("MAX_MEDIA_DURATION must be a positive duration")
	}
	if cfg.JobStaleAfter, err = time.ParseDuration(valueOrDefault("JOB_STALE_AFTER", "30m")); err != nil || cfg.JobStaleAfter <= 0 {
		return Config{}, fmt.Errorf("JOB_STALE_AFTER must be a positive duration")
	}
	if cfg.MaxConcurrentJobs, err = strconv.Atoi(valueOrDefault("MAX_CONCURRENT_JOBS", "4")); err != nil || cfg.MaxConcurrentJobs < 1 || cfg.MaxConcurrentJobs > 64 {
		return Config{}, fmt.Errorf("MAX_CONCURRENT_JOBS must be an integer between 1 and 64")
	}
	if cfg.StatsCacheTTL, err = time.ParseDuration(valueOrDefault("STATS_CACHE_TTL", "5m")); err != nil || cfg.StatsCacheTTL <= 0 {
		return Config{}, fmt.Errorf("STATS_CACHE_TTL must be a positive duration")
	}
	cfg.StatsTimezone = valueOrDefault("STATS_TIMEZONE", "Europe/Kyiv")
	if !validTimezone(cfg.StatsTimezone) {
		return Config{}, fmt.Errorf("STATS_TIMEZONE must be an IANA zone name such as Europe/Kyiv")
	}
	if !strings.HasPrefix(cfg.TelegramAPIBase, "http://") && !strings.HasPrefix(cfg.TelegramAPIBase, "https://") {
		return Config{}, fmt.Errorf("TELEGRAM_API_BASE must be an http or https URL")
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

// validTimezone keeps the value inside the IANA character set. PostgreSQL is
// the one that resolves the zone, and it receives the name as a bound
// parameter, so this is a readability guard rather than a security boundary.
func validTimezone(tz string) bool {
	if tz == "" || len(tz) > 64 {
		return false
	}
	for _, r := range tz {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '/', r == '_', r == '-', r == '+':
		default:
			return false
		}
	}
	return true
}

func valueOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

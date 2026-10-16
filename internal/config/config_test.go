// SPDX-License-Identifier: Apache-2.0
package config

import (
	"testing"
	"time"
)

func TestLoadRequiresSecrets(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("DEEPGRAM_API_KEY", "")
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing TELEGRAM_BOT_TOKEN")
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing DEEPGRAM_API_KEY")
	}
	t.Setenv("DEEPGRAM_API_KEY", "dg")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing DATABASE_URL")
	}
	t.Setenv("DATABASE_URL", "postgres://x")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.TranscriptRetention == 0 || cfg.MaxMediaBytes != 20<<20 || cfg.MaxMediaDuration.String() != "1h0m0s" {
		t.Fatalf("defaults: %+v", cfg)
	}
}

func TestLoadRejectsInvalidMediaLimits(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok")
	t.Setenv("DEEPGRAM_API_KEY", "dg")
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("MAX_MEDIA_BYTES", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid MAX_MEDIA_BYTES")
	}
	t.Setenv("MAX_MEDIA_BYTES", "1024")
	t.Setenv("MAX_MEDIA_DURATION", "nope")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid MAX_MEDIA_DURATION")
	}
}

func TestJobStaleAfterDefaultsAndValidates(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "t")
	t.Setenv("DEEPGRAM_API_KEY", "k")
	t.Setenv("DATABASE_URL", "postgres://localhost/voicy")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JobStaleAfter != 30*time.Minute {
		t.Fatalf("JobStaleAfter = %s", cfg.JobStaleAfter)
	}

	t.Setenv("JOB_STALE_AFTER", "-5m")
	if _, err := Load(); err == nil {
		t.Fatal("a non-positive JOB_STALE_AFTER must be rejected")
	}
}

func TestConcurrencyAndStatsDefaults(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "t")
	t.Setenv("DEEPGRAM_API_KEY", "k")
	t.Setenv("DATABASE_URL", "postgres://localhost/voicy")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConcurrentJobs != 4 {
		t.Fatalf("MaxConcurrentJobs = %d", cfg.MaxConcurrentJobs)
	}
	if cfg.StatsTimezone != "Europe/Kyiv" || cfg.StatsCacheTTL != 5*time.Minute {
		t.Fatalf("stats defaults = %q / %s", cfg.StatsTimezone, cfg.StatsCacheTTL)
	}

	for _, tc := range []struct{ key, value string }{
		{"MAX_CONCURRENT_JOBS", "0"},
		{"MAX_CONCURRENT_JOBS", "200"},
		{"MAX_CONCURRENT_JOBS", "many"},
		{"STATS_TIMEZONE", "Europe/Kyiv; DROP TABLE jobs"},
		{"STATS_CACHE_TTL", "0s"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatalf("%s=%q must be rejected", tc.key, tc.value)
			}
		})
	}
}

func TestUserDailyAudioLimit(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "t")
	t.Setenv("DEEPGRAM_API_KEY", "k")
	t.Setenv("DATABASE_URL", "postgres://localhost/voicy")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserDailyAudioLimit != time.Hour {
		t.Fatalf("default UserDailyAudioLimit = %s, want 1h", cfg.UserDailyAudioLimit)
	}

	t.Setenv("USER_DAILY_AUDIO_LIMIT", "0")
	if cfg, err = Load(); err != nil || cfg.UserDailyAudioLimit != 0 {
		t.Fatalf("0 must turn the limit off: %s, %v", cfg.UserDailyAudioLimit, err)
	}

	t.Setenv("USER_DAILY_AUDIO_LIMIT", "3h")
	if cfg, err = Load(); err != nil || cfg.UserDailyAudioLimit != 3*time.Hour {
		t.Fatalf("3h: %s, %v", cfg.UserDailyAudioLimit, err)
	}

	for _, tc := range []struct{ limit, maxDuration string }{
		{"-1h", "1h"},
		{"lots", "1h"},
		// Shorter than the longest file Voicy accepts: that file could never fit.
		{"30m", "1h"},
	} {
		t.Run(tc.limit, func(t *testing.T) {
			t.Setenv("USER_DAILY_AUDIO_LIMIT", tc.limit)
			t.Setenv("MAX_MEDIA_DURATION", tc.maxDuration)
			if _, err := Load(); err == nil {
				t.Fatalf("USER_DAILY_AUDIO_LIMIT=%q with MAX_MEDIA_DURATION=%q must be rejected", tc.limit, tc.maxDuration)
			}
		})
	}

	t.Setenv("USER_DAILY_AUDIO_LIMIT", "30m")
	t.Setenv("MAX_MEDIA_DURATION", "30m")
	if _, err := Load(); err != nil {
		t.Fatalf("a limit equal to MAX_MEDIA_DURATION is valid: %v", err)
	}
}

// A self-hosted Bot API server runs with --local: it answers getFile with an
// absolute path on its own disk and serves nothing over HTTP. Without a
// mounted data directory every transcription would fail on download, so the
// misconfiguration has to be caught before the process starts.
func TestLoadRequiresBotAPIFilesDirForASelfHostedServer(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok")
	t.Setenv("DEEPGRAM_API_KEY", "dg")
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("TELEGRAM_API_BASE", "http://telegram-bot-api:8081")
	t.Setenv("BOT_API_FILES_DIR", "")

	if _, err := Load(); err == nil {
		t.Fatal("want an error naming BOT_API_FILES_DIR")
	}

	t.Setenv("BOT_API_FILES_DIR", "/var/lib/telegram-bot-api")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BotAPIFilesDir != "/var/lib/telegram-bot-api" {
		t.Fatalf("BotAPIFilesDir = %q", cfg.BotAPIFilesDir)
	}

	// Telegram's own server needs none of this.
	t.Setenv("TELEGRAM_API_BASE", DefaultTelegramAPIBase)
	t.Setenv("BOT_API_FILES_DIR", "")
	if _, err := Load(); err != nil {
		t.Fatalf("the cloud endpoint must not require a files directory: %v", err)
	}
}

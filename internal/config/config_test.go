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

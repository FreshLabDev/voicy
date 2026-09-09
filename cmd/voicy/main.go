// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FreshLabDev/tg"
	"github.com/FreshLabDev/voicy/internal/bot"
	"github.com/FreshLabDev/voicy/internal/config"
	"github.com/FreshLabDev/voicy/internal/db"
	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/health"
	"github.com/FreshLabDev/voicy/internal/media"
	"github.com/FreshLabDev/voicy/internal/metrics"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(os.Getenv("LOG_LEVEL"))}))
	if err := run(log); err != nil {
		log.Error("exit", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := connect(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer store.Close()
	if cfg.AutoMigrate {
		if err := store.Migrate(ctx, cfg.MigrationsDir); err != nil {
			return err
		}
	}

	client := tg.New(cfg.TelegramBotToken,
		tg.WithAPIBase(cfg.TelegramAPIBase),
		// my_chat_member is what tells shared Core where Voicy lives; Telegram
		// leaves it out of the default set, so it has to be asked for.
		tg.WithAllowedUpdates("message", "callback_query", "my_chat_member"),
		tg.WithLocalFiles(cfg.BotAPIFilesDir),
		tg.WithLogger(log),
		tg.WithObserver(func(e tg.Event) {
			switch {
			// A capability probe is answered with a parameter error on
			// purpose; counting it would add a phantom incident per probed
			// method to every start.
			case e.Probe, e.Err == nil:
			case e.Status == http.StatusTooManyRequests:
				metrics.TelegramLimited.Inc(e.Method)
			default:
				metrics.TelegramErrors.Inc(e.Method)
			}
		}),
	)
	selfHosted := cfg.TelegramAPIBase != config.DefaultTelegramAPIBase
	if selfHosted {
		// A local server lifts the cloud API's 20 MB getFile ceiling and hands
		// over files by absolute path on its own disk.
		log.Info("using a self-hosted bot api server", "base", cfg.TelegramAPIBase)
	}
	// Voicy without rich messages is a bot that receives a voice message and
	// answers nothing: every transcript, placeholder and ephemeral reply goes
	// through them. A server that lacks them is a startup failure, not a
	// surprise on the first circle.
	me, err := client.Preflight(ctx, tg.Needs{
		Methods: []string{"sendRichMessage", "editEphemeralMessageText"},
		Files:   selfHosted,
		Wait:    cfg.TelegramReadyWait,
	})
	if err != nil {
		return err
	}
	log.Info("telegram preflight passed", "username", me.Username, "bot_api", tg.BotAPI)
	stt := deepgram.New(cfg.DeepgramAPIKey)
	store.SetStatsTimezone(cfg.StatsTimezone)
	b := bot.New(store, client, stt, log)
	b.SetVersion(version)
	b.SetMediaLimits(cfg.MaxMediaBytes, cfg.MaxMediaDuration)
	b.SetWorkers(cfg.MaxConcurrentJobs)
	b.SetStatsTTL(cfg.StatsCacheTTL)
	extractor := media.NewExtractor(cfg.FFmpegPath)
	if !extractor.Available() && cfg.FFmpegPath != "" {
		// Not fatal: voice messages and audio files still work. Video will not.
		log.Warn("ffmpeg not found; video will be sent to Deepgram unextracted", "path", cfg.FFmpegPath)
	}
	b.SetMediaTools(extractor, cfg.MediaTmpDir, cfg.ExtractAboveBytes)

	started := time.Now()
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler())
	mux.Handle("/healthz", health.New(store, b.LastPoll, b.Initialized, started, cfg.JobStaleAfter, health.Build{
		Version: version, Commit: commit, Date: date,
	}, log))
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go func() {
		log.Info("telegram polling starting", "workers", cfg.MaxConcurrentJobs)
		if err := b.Run(ctx); err != nil {
			errCh <- err
		}
	}()
	go cleanupLoop(ctx, store, cfg.TranscriptRetention, log)
	go reaperLoop(ctx, store, cfg.JobStaleAfter, log)

	select {
	case <-ctx.Done():
	case err := <-errCh:
		stop()
		_ = srv.Shutdown(context.Background())
		return err
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// connect retries the first PostgreSQL connection. Container DNS inside the
// shared core_net is not always resolvable the instant the process starts, and
// exiting there turns a two-second blip into a restart loop.
func connect(ctx context.Context, url string, log *slog.Logger) (*db.Store, error) {
	const maxAttempts = 8
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		store, err := db.Connect(ctx, url)
		if err == nil {
			return store, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		delay := time.Duration(1<<min(attempt-1, 4)) * time.Second
		log.Warn("database connect failed; retrying", "attempt", attempt, "retry_in", delay, "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}

// reaperLoop closes jobs abandoned by a crashed or killed process. Nothing else
// moves a received row to a terminal state, so without this one interrupted
// transcription would hold /healthz at 503 for the life of the deployment.
func reaperLoop(ctx context.Context, store *db.Store, staleAfter time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		reapCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		reaped, err := store.ReapStaleJobs(reapCtx, staleAfter)
		cancel()
		switch {
		case err != nil:
			log.Warn("stale job reaper failed", "error", err)
		case reaped > 0:
			metrics.StaleJobsReaped.Add(reaped)
			log.Warn("failed stale jobs", "count", reaped, "stale_after", staleAfter.String())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func cleanupLoop(ctx context.Context, store *db.Store, retention time.Duration, log *slog.Logger) {
	run := func() {
		cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		deleted, err := store.Cleanup(cleanupCtx, retention)
		if err != nil {
			log.Warn("database cleanup failed", "error", err)
			return
		}
		if deleted > 0 {
			log.Info("database cleanup completed", "deleted", deleted)
		}
	}
	run()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

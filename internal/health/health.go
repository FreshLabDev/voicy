// SPDX-License-Identifier: Apache-2.0
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/FreshLabDev/voicy/internal/db"
)

type Store interface {
	HealthStatus(context.Context) (db.HealthStatus, error)
}

type Build struct {
	Version string
	Commit  string
	Date    string
}

type Handler struct {
	store       Store
	lastPoll    func() time.Time
	initialized func() bool
	startedAt   time.Time
	build       Build
	log         *slog.Logger
}

func New(store Store, lastPoll func() time.Time, initialized func() bool, startedAt time.Time, build Build, log *slog.Logger) *Handler {
	return &Handler{store: store, lastPoll: lastPoll, initialized: initialized, startedAt: startedAt, build: build, log: log}
}

type response struct {
	OK                   bool       `json:"ok"`
	Version              string     `json:"version"`
	Commit               string     `json:"commit"`
	BuiltAt              string     `json:"built_at"`
	DB                   bool       `json:"db"`
	TelegramInitialized  bool       `json:"telegram_initialized"`
	TelegramPollingFresh bool       `json:"telegram_polling_fresh"`
	TelegramLastPollAt   *time.Time `json:"telegram_last_poll_at,omitempty"`
	JobsReceived         int64      `json:"jobs_received"`
	JobsStuckReceived    int64      `json:"jobs_stuck_received"`
	JobsFailed           int64      `json:"jobs_failed"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	result := response{Version: h.build.Version, Commit: h.build.Commit, BuiltAt: h.build.Date}
	status, err := h.store.HealthStatus(ctx)
	if err != nil {
		h.log.Error("health database check failed", "error", err)
	} else {
		result.DB = true
		result.JobsReceived = status.Received
		result.JobsStuckReceived = status.StuckReceived
		result.JobsFailed = status.Failed
	}
	lastPoll := h.lastPoll()
	if !lastPoll.IsZero() {
		result.TelegramLastPollAt = &lastPoll
	}
	result.TelegramPollingFresh = fresh(h.startedAt, lastPoll, 90*time.Second)
	result.TelegramInitialized = h.initialized != nil && h.initialized()
	result.OK = result.DB && result.TelegramInitialized && result.TelegramPollingFresh && result.JobsStuckReceived == 0
	w.Header().Set("Content-Type", "application/json")
	if !result.OK {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(result)
}

func fresh(startedAt, lastSeen time.Time, maxAge time.Duration) bool {
	if lastSeen.IsZero() {
		return time.Since(startedAt) < maxAge
	}
	return time.Since(lastSeen) < maxAge
}

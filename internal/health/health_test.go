// SPDX-License-Identifier: Apache-2.0
package health

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/FreshLabDev/voicy/internal/db"
)

type fakeStore struct {
	status db.HealthStatus
	err    error
	stale  time.Duration
}

func (f *fakeStore) HealthStatus(_ context.Context, staleAfter time.Duration) (db.HealthStatus, error) {
	f.stale = staleAfter
	return f.status, f.err
}

func TestHealthyRequiresDatabaseTelegramAndFreshPolling(t *testing.T) {
	now := time.Now()
	store := &fakeStore{status: db.HealthStatus{Received: 1, Failed: 2}}
	h := New(
		store,
		func() time.Time { return now },
		func() bool { return true },
		now.Add(-time.Minute),
		20*time.Minute,
		Build{Version: "v1", Commit: "abc", Date: "today"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got response
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || !got.DB || !got.TelegramInitialized || !got.TelegramPollingFresh || got.JobsReceived != 1 || got.JobsFailed != 2 {
		t.Fatalf("response = %+v", got)
	}
	if store.stale != 20*time.Minute {
		t.Fatalf("stale threshold = %s, want the configured one", store.stale)
	}
}

func TestUnhealthyForStuckJobOrMissingInitialization(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name        string
		initialized bool
		stuck       int64
	}{
		{name: "not initialized"},
		{name: "stuck job", initialized: true, stuck: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := New(
				&fakeStore{status: db.HealthStatus{StuckReceived: tc.stuck}},
				func() time.Time { return now },
				func() bool { return tc.initialized },
				now.Add(-time.Minute), 0, Build{},
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if rr.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
			}
		})
	}
}

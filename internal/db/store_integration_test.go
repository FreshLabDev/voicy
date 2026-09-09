// SPDX-License-Identifier: Apache-2.0
//go:build integration

// Package db's SQL is the part of Voicy that unit tests cannot reach: job
// idempotency, the atomic completion-plus-statistics transaction, cache
// variants, and the reaper all live in PostgreSQL. These tests run against a
// real server.
//
//	VOICY_TEST_DATABASE_URL=postgres://… go test -tags=integration ./internal/db/
package db

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"
	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/settings"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("VOICY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set VOICY_TEST_DATABASE_URL to run the database tests")
	}
	// These tests truncate every table they touch. The production database is
	// named "core" and is shared with the other bots, so refuse anything that is
	// not visibly a scratch database.
	if !strings.Contains(databaseName(url), "test") {
		t.Fatalf("VOICY_TEST_DATABASE_URL must point at a disposable database whose name contains \"test\", got %q", databaseName(url))
	}
	ctx := context.Background()

	// The schema mirrors production: domain tables in voicy, identity in core.
	bootstrap, err := os.ReadFile(filepath.Join("..", "..", "deploy", "core-init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(store.Close)
	if _, err := store.pool.Exec(ctx, string(bootstrap)); err != nil {
		t.Fatalf("core bootstrap: %v", err)
	}
	if err := store.Migrate(ctx, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	truncate(t, store)
	t.Cleanup(func() { truncate(t, store) })
	return store
}

// databaseName extracts the database from a PostgreSQL URL without needing the
// credentials in it.
func databaseName(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(parsed.Path, "/")
}

func truncate(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range []string{
		`TRUNCATE voicy.jobs, voicy.user_stats, voicy.user_settings, voicy.transcripts`,
		`TRUNCATE core.person CASCADE`,
		// The language hub has no foreign key to core.person, so the CASCADE
		// above leaves a preference behind for the next test to trip over.
		`TRUNCATE core.user_language`,
		`UPDATE voicy.runtime_state SET telegram_offset = 0 WHERE singleton`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func person(t *testing.T, s *Store, id int64) {
	t.Helper()
	if err := s.Touch(context.Background(), tg.User{ID: id, FirstName: "T"}, nil); err != nil {
		t.Fatalf("touch: %v", err)
	}
}

func result(text string, words int, seconds float64) deepgram.Result {
	return deepgram.Result{Text: text, WordCount: words, Duration: seconds, Language: "en"}
}

// Telegram redelivers an unconfirmed update after a restart. The second
// CreateJob must return the original row, not create a competing one.
func TestCreateJobIsIdempotentPerUpdate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	person(t, s, 1)

	first, err := s.CreateJob(ctx, 900, 1, 1, 1, "voice", "F1", settings.DefaultVariant, "token-one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateJob(ctx, 900, 1, 1, 1, "voice", "F1", settings.DefaultVariant, "token-two")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("a repeated update made a new job: %d vs %d", first.ID, second.ID)
	}
	if second.RetrievalToken != first.RetrievalToken {
		t.Fatalf("the retrieval token must survive a replay: %q vs %q", second.RetrievalToken, first.RetrievalToken)
	}
	if second.Delivered {
		t.Fatal("a fresh job cannot be delivered")
	}
}

// The terminal transition and the statistics increment share one transaction
// and must happen at most once, however many times CompleteJob is called.
func TestCompleteJobCountsOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	person(t, s, 2)

	job, err := s.CreateJob(ctx, 901, 1, 2, 2, "voice", "F2", settings.DefaultVariant, "tok2")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := s.CompleteJob(ctx, job.ID, "sent", "", false, result("hello there", 2, 4)); err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	snap, err := s.UserStats(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Transcriptions != 1 || snap.Words != 2 || snap.Voice != 1 {
		t.Fatalf("statistics counted a repeat: %+v", snap)
	}
}

// Empty and failed runs are kept for operations but must never reach the
// user-facing counters.
func TestCompleteJobSkipsEmptyAndFailed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	person(t, s, 3)

	empty, _ := s.CreateJob(ctx, 902, 1, 3, 3, "voice", "F3", settings.DefaultVariant, "tok3")
	if err := s.CompleteJob(ctx, empty.ID, "empty", "empty", false, result("", 0, 2)); err != nil {
		t.Fatal(err)
	}
	failed, _ := s.CreateJob(ctx, 903, 1, 3, 3, "voice", "F4", settings.DefaultVariant, "tok4")
	if err := s.CompleteJob(ctx, failed.ID, "failed", "deepgram", false, deepgram.Result{}); err != nil {
		t.Fatal(err)
	}
	// A "sent" row whose transcript is blank is still not a transcription.
	blank, _ := s.CreateJob(ctx, 904, 1, 3, 3, "voice", "F5", settings.DefaultVariant, "tok5")
	if err := s.CompleteJob(ctx, blank.ID, "sent", "", false, result("   ", 0, 1)); err != nil {
		t.Fatal(err)
	}

	snap, err := s.UserStats(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Transcriptions != 0 {
		t.Fatalf("unsuccessful runs reached the counters: %+v", snap)
	}
}

// One file has one transcript per Deepgram option set. Mixing them would serve
// a user someone else's formatting.
func TestCacheIsKeyedByVariant(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.SaveTranscript(ctx, "SAME", "U", "voice", settings.DefaultVariant, result("plain", 1, 3)); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTranscript(ctx, "SAME", "U", "voice", "sf1-p1-fw0-pf0-d1", result("with speakers", 2, 3)); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetCached(ctx, "SAME", settings.DefaultVariant)
	if err != nil || !ok {
		t.Fatalf("default variant lookup: ok=%v err=%v", ok, err)
	}
	if got.Transcript != "plain" {
		t.Fatalf("variants leaked into each other: %q", got.Transcript)
	}
	if _, ok, _ := s.GetCached(ctx, "SAME", "sf0-p0-fw0-pf0-d0"); ok {
		t.Fatal("an unknown variant must be a miss, not a hit")
	}
}

// A crashed process leaves jobs in received forever. Nothing else moves them,
// so /healthz would report a stuck job for the life of the deployment.
func TestReapStaleJobs(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	person(t, s, 4)

	fresh, _ := s.CreateJob(ctx, 905, 1, 4, 4, "voice", "F6", settings.DefaultVariant, "tok6")
	stale, _ := s.CreateJob(ctx, 906, 1, 4, 4, "voice", "F7", settings.DefaultVariant, "tok7")
	if _, err := s.pool.Exec(ctx, `UPDATE jobs SET created_at = now() - interval '2 hours' WHERE id=$1`, stale.ID); err != nil {
		t.Fatal(err)
	}

	reaped, err := s.ReapStaleJobs(ctx, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if reaped != 1 {
		t.Fatalf("reaped = %d, want only the stale job", reaped)
	}
	health, err := s.HealthStatus(ctx, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if health.StuckReceived != 0 {
		t.Fatalf("health still reports %d stuck jobs", health.StuckReceived)
	}
	if health.Received != 1 {
		t.Fatalf("the fresh job must survive, received = %d", health.Received)
	}
	_ = fresh
}

// MarkDelivered is what lets a retry tell "already sent" from "never sent".
func TestMarkDeliveredSurvivesReplay(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	person(t, s, 5)

	job, err := s.CreateJob(ctx, 907, 1, 5, 5, "voice", "F8", settings.DefaultVariant, "tok8")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDelivered(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	replay, err := s.CreateJob(ctx, 907, 1, 5, 5, "voice", "F8", settings.DefaultVariant, "tok8")
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Delivered {
		t.Fatal("a replayed update must see that the transcript was already sent")
	}
	if replay.Status != "received" {
		t.Fatalf("status = %q; delivery is not completion", replay.Status)
	}
}

// The deep link is owner-bound: another user's token must not resolve.
func TestTranscriptByTokenIsOwnerBound(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	person(t, s, 6)
	person(t, s, 7)

	job, _ := s.CreateJob(ctx, 908, 1, 6, 6, "voice", "F9", settings.DefaultVariant, "secret-token-value")
	if err := s.SaveTranscript(ctx, "F9", "U9", "voice", settings.DefaultVariant, result("private text", 2, 5)); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteJob(ctx, job.ID, "sent", "", false, result("private text", 2, 5)); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := s.TranscriptByToken(ctx, 7, "secret-token-value"); err != nil || ok {
		t.Fatalf("another user resolved the token: ok=%v err=%v", ok, err)
	}
	got, ok, err := s.TranscriptByToken(ctx, 6, "secret-token-value")
	if err != nil || !ok {
		t.Fatalf("owner lookup: ok=%v err=%v", ok, err)
	}
	if got.Transcript != "private text" {
		t.Fatalf("transcript = %q", got.Transcript)
	}
}

// Settings are toggled by a callback that carries the desired value, so a
// replayed callback must land on the same state rather than flipping twice.
func TestSetSettingIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	person(t, s, 8)

	for i := 0; i < 2; i++ {
		got, err := s.SetSetting(ctx, 8, "diarize", true)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Diarize {
			t.Fatalf("attempt %d left diarize off", i)
		}
	}
	got, err := s.UserSettings(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Diarize || !got.SmartFormat {
		t.Fatalf("settings = %+v; untouched keys must keep their defaults", got)
	}
}

func TestOffsetOnlyMovesForward(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.AdvanceOffset(ctx, 500); err != nil {
		t.Fatal(err)
	}
	if err := s.AdvanceOffset(ctx, 100); err != nil {
		t.Fatal(err)
	}
	got, err := s.Offset(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != 500 {
		t.Fatalf("offset went backwards to %d", got)
	}
}

// Retention removes terminal jobs and unused transcripts, and must leave a
// transcript that is still being served alone.
func TestCleanupKeepsRecentlyUsedTranscripts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	person(t, s, 9)

	if err := s.SaveTranscript(ctx, "OLD", "UO", "voice", settings.DefaultVariant, result("old", 1, 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTranscript(ctx, "NEW", "UN", "voice", settings.DefaultVariant, result("new", 1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE transcripts SET last_used_at = now() - interval '100 days' WHERE file_id='OLD'`); err != nil {
		t.Fatal(err)
	}
	job, _ := s.CreateJob(ctx, 909, 1, 9, 9, "voice", "OLD", settings.DefaultVariant, "tok9")
	if err := s.CompleteJob(ctx, job.ID, "sent", "", false, result("old", 1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE jobs SET finished_at = now() - interval '100 days' WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}

	deleted, err := s.Cleanup(ctx, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want the old job and the old transcript", deleted)
	}
	if _, ok, _ := s.GetCached(ctx, "NEW", settings.DefaultVariant); !ok {
		t.Fatal("a recently used transcript was deleted")
	}
}

// The language hub is the one piece of Voicy's state that lives outside its own
// schema, so the round trip is worth proving against a real server: a manual
// pick outranks the Telegram hint, and clearing it hands the answer back.
func TestClearLanguageRestoresTheTelegramHint(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.Touch(ctx, tg.User{ID: 11, FirstName: "T", LanguageCode: "de-DE"}, nil); err != nil {
		t.Fatal(err)
	}
	if lang, ok, err := s.EffectiveLanguage(ctx, 11); err != nil || !ok || lang != "de" {
		t.Fatalf("client hint = %q %v %v, want de", lang, ok, err)
	}
	if err := s.SetLanguage(ctx, 11, "uk"); err != nil {
		t.Fatal(err)
	}
	if lang, ok, err := s.EffectiveLanguage(ctx, 11); err != nil || !ok || lang != "uk" {
		t.Fatalf("manual pick = %q %v %v, want uk", lang, ok, err)
	}
	if err := s.ClearLanguage(ctx, 11); err != nil {
		t.Fatal(err)
	}
	if lang, ok, err := s.EffectiveLanguage(ctx, 11); err != nil || !ok || lang != "de" {
		t.Fatalf("after clearing = %q %v %v, want the de hint back", lang, ok, err)
	}
	// Clearing a preference nobody set is not an error: the button is drawn
	// whether or not there is anything to undo.
	if err := s.ClearLanguage(ctx, 11); err != nil {
		t.Fatalf("second clear: %v", err)
	}
}

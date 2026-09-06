// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"testing"
	"time"

	"github.com/FreshLabDev/voicy/internal/decide"
	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/stats"
	"github.com/FreshLabDev/voicy/internal/transcript"
)

// A direct chat used to show nothing at all while Deepgram worked: the single
// chat action expires after five seconds. Now the user gets a message right
// away and the result replaces it.
func TestDirectChatShowsPlaceholderThenReplacesIt(t *testing.T) {
	tg := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "spoken words"}}
	b := New(&fakeStore{}, tg, stt, logger())
	upd := parseUpd(t, `{
	  "update_id": 60,
	  "message": {
	    "message_id": 12,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "voice": {"file_id": "DM1", "file_unique_id": "UD", "duration": 30}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if len(tg.sent) != 1 || tg.sent[0] != transcript.WorkingText("en") {
		t.Fatalf("placeholder = %v", tg.sent)
	}
	if len(tg.richEdits) != 1 || !contains(tg.richEdits[0], "spoken words") {
		t.Fatalf("the placeholder must become the transcript, edits = %v", tg.richEdits)
	}
	if tg.calls[0] != "sendRich" {
		t.Fatalf("the placeholder must come first, calls = %v", tg.calls)
	}
	if tg.chatActions == 0 {
		t.Fatal("a typing indicator must be shown while transcribing")
	}
}

// An error replaces the placeholder too, instead of leaving "Transcribing…"
// stranded above a separate failure message.
func TestDirectChatFailureReplacesPlaceholder(t *testing.T) {
	tg := &fakeTG{}
	stt := &countingSTT{err: context.DeadlineExceeded}
	b := New(&fakeStore{}, tg, stt, logger())
	upd := parseUpd(t, `{
	  "update_id": 61,
	  "message": {
	    "message_id": 13,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "voice": {"file_id": "DM2", "file_unique_id": "UE", "duration": 5}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if len(tg.richEdits) != 1 || tg.richEdits[0] != transcript.ErrorText("en") {
		t.Fatalf("edits = %v", tg.richEdits)
	}
	if len(tg.sent) != 1 {
		t.Fatalf("the failure must not add a second message: %v", tg.sent)
	}
}

// The pulse must not outlive the job, or a stale "typing" status keeps blinking
// after the transcript has landed.
func TestPulseStopsWithTheJob(t *testing.T) {
	tg := &fakeTG{}
	b := New(&fakeStore{}, tg, &countingSTT{}, logger())
	b.pulseEvery = 5 * time.Millisecond
	p := &progress{}
	p.startPulse(context.Background(), b, decide.Action{ChatID: 7})

	deadline := time.Now().Add(2 * time.Second)
	for {
		tg.mu.Lock()
		n := tg.chatActions
		tg.mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the indicator must repeat, saw %d actions", n)
		}
		time.Sleep(2 * time.Millisecond)
	}
	// stopPulse waits for the goroutine, so nothing can fire after it returns.
	p.stopPulse()
	tg.mu.Lock()
	before := tg.chatActions
	tg.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	tg.mu.Lock()
	after := tg.chatActions
	tg.mu.Unlock()
	if after != before {
		t.Fatalf("chat actions kept firing after stop: %d -> %d", before, after)
	}
	// Stopping twice must be safe: failJob stops the pulse and deliver may too.
	p.stopPulse()
}

// countingStore records how often the statistics queries actually run.
type countingStore struct {
	fakeStore
	userCalls   int
	globalCalls int
}

func (c *countingStore) UserStats(context.Context, int64) (stats.Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.userCalls++
	return stats.Snapshot{Transcriptions: 3, PeakHour: 9}, nil
}

func (c *countingStore) GlobalStats(context.Context) (stats.Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.globalCalls++
	return stats.Snapshot{Transcriptions: 30, Users: 4, PeakHour: 11}, nil
}

// Toggling My/All used to run the peak-hour scan on every tap.
func TestStatsPanelIsServedFromCache(t *testing.T) {
	st := &countingStore{}
	b := New(st, &fakeTG{}, &countingSTT{}, logger())
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := b.snapshot(ctx, 7, false); err != nil {
			t.Fatal(err)
		}
		if _, err := b.snapshot(ctx, 7, true); err != nil {
			t.Fatal(err)
		}
	}
	if st.userCalls != 1 || st.globalCalls != 1 {
		t.Fatalf("ten taps ran %d personal and %d global scans, want one each", st.userCalls, st.globalCalls)
	}
}

func TestStatsCacheKeepsUsersApart(t *testing.T) {
	st := &countingStore{}
	b := New(st, &fakeTG{}, &countingSTT{}, logger())
	ctx := context.Background()
	if _, err := b.snapshot(ctx, 7, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.snapshot(ctx, 8, false); err != nil {
		t.Fatal(err)
	}
	if st.userCalls != 2 {
		t.Fatalf("two different users must not share a snapshot, calls = %d", st.userCalls)
	}
}

// An expired entry is still shown while the refresh runs behind the reply.
func TestStatsCacheServesStaleWhileRefreshing(t *testing.T) {
	c := newStatsCache(time.Minute)
	now := time.Now()
	c.put(1, stats.Snapshot{Transcriptions: 5}, now.Add(-2*time.Minute))

	snap, fresh, ok := c.get(1, now)
	if !ok || fresh {
		t.Fatalf("expected a stale hit, ok=%v fresh=%v", ok, fresh)
	}
	if snap.Transcriptions != 5 {
		t.Fatalf("stale value lost: %+v", snap)
	}
	if !c.beginRefresh(1) {
		t.Fatal("first caller must win the refresh")
	}
	if c.beginRefresh(1) {
		t.Fatal("a second caller must not stampede the same key")
	}
	c.endRefresh(1)
	if !c.beginRefresh(1) {
		t.Fatal("the key must be claimable again once the refresh ends")
	}
}

// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/FreshLabDev/tg"
	"github.com/FreshLabDev/voicy/internal/deepgram"
)

// blockingSTT parks every call until release is closed, and reports how many
// calls are parked at once.
type blockingSTT struct {
	mu      sync.Mutex
	entered int
	peak    int
	order   []string
	release chan struct{}
	arrived chan struct{}
}

func newBlockingSTT() *blockingSTT {
	return &blockingSTT{release: make(chan struct{}), arrived: make(chan struct{}, 64)}
}

func (s *blockingSTT) Transcribe(ctx context.Context, _ string, _ string, _ deepgram.Options) (deepgram.Result, error) {
	s.mu.Lock()
	s.entered++
	if s.entered > s.peak {
		s.peak = s.entered
	}
	s.mu.Unlock()
	s.arrived <- struct{}{}
	select {
	case <-s.release:
	case <-ctx.Done():
		return deepgram.Result{}, ctx.Err()
	}
	s.mu.Lock()
	s.entered--
	s.mu.Unlock()
	return deepgram.Result{Text: "done"}, nil
}

func voiceUpdate(t *testing.T, updateID, userID int64, fileID string) tg.Update {
	t.Helper()
	raw := `{
	  "update_id": ` + strconv.FormatInt(updateID, 10) + `,
	  "message": {
	    "message_id": ` + strconv.FormatInt(updateID, 10) + `,
	    "from": {"id": ` + strconv.FormatInt(userID, 10) + `, "is_bot": false, "first_name": "A"},
	    "chat": {"id": ` + strconv.FormatInt(userID, 10) + `, "type": "private"},
	    "voice": {"file_id": "` + fileID + `", "file_unique_id": "u` + fileID + `", "duration": 3}
	  }
	}`
	var upd tg.Update
	if err := json.Unmarshal([]byte(raw), &upd); err != nil {
		t.Fatal(err)
	}
	return upd
}

// The whole point of the pool: one slow recording must not make everyone else
// wait. Before this, updates were handled strictly one at a time.
func TestBatchRunsDifferentUsersConcurrently(t *testing.T) {
	stt := newBlockingSTT()
	b := New(&fakeStore{}, &fakeTG{}, stt, logger())
	b.SetWorkers(3)

	updates := []tg.Update{
		voiceUpdate(t, 1, 11, "A"),
		voiceUpdate(t, 2, 22, "B"),
		voiceUpdate(t, 3, 33, "C"),
	}
	done := make(chan struct{})
	go func() {
		b.processBatch(context.Background(), updates)
		close(done)
	}()

	// All three must be inside Deepgram at the same time before any returns.
	for i := 0; i < len(updates); i++ {
		select {
		case <-stt.arrived:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d updates started; the batch is still serialized", i, len(updates))
		}
	}
	close(stt.release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("processBatch did not finish")
	}
	if stt.peak < 3 {
		t.Fatalf("peak concurrency = %d, want 3", stt.peak)
	}
}

// Two voices from one person share a job row and a chat: they must stay in
// arrival order on one goroutine, however many workers are free.
func TestBatchSerializesOneUser(t *testing.T) {
	stt := newBlockingSTT()
	close(stt.release) // never block, just record ordering
	api := &fakeTG{}
	b := New(&fakeStore{}, api, stt, logger())
	b.SetWorkers(4)

	updates := []tg.Update{
		voiceUpdate(t, 1, 77, "FIRST"),
		voiceUpdate(t, 2, 77, "SECOND"),
		voiceUpdate(t, 3, 77, "THIRD"),
	}
	b.processBatch(context.Background(), updates)

	if stt.peak > 1 {
		t.Fatalf("one user ran %d transcriptions at once", stt.peak)
	}
	if got := len(api.transcriptParts()); got != 3 {
		t.Fatalf("delivered %d transcripts, want 3", got)
	}
}

func TestGroupByUserKeepsOrder(t *testing.T) {
	updates := []tg.Update{
		voiceUpdate(t, 1, 11, "A"),
		voiceUpdate(t, 2, 22, "B"),
		voiceUpdate(t, 3, 11, "C"),
		voiceUpdate(t, 4, 22, "D"),
		voiceUpdate(t, 5, 11, "E"),
	}
	groups := groupByUser(updates)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want one per user", len(groups))
	}
	if len(groups[0]) != 3 || groups[0][0].UpdateID != 1 || groups[0][2].UpdateID != 5 {
		t.Fatalf("first user's updates lost their order: %+v", groups[0])
	}
	if len(groups[1]) != 2 || groups[1][0].UpdateID != 2 || groups[1][1].UpdateID != 4 {
		t.Fatalf("second user's updates lost their order: %+v", groups[1])
	}
}

// A cancelled context must unwind the batch instead of holding the poll loop.
func TestBatchStopsOnContextCancel(t *testing.T) {
	stt := newBlockingSTT()
	b := New(&fakeStore{}, &fakeTG{}, stt, logger())
	b.SetWorkers(2)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		b.processBatch(ctx, []tg.Update{
			voiceUpdate(t, 1, 11, "A"),
			voiceUpdate(t, 2, 22, "B"),
		})
		close(done)
	}()
	<-stt.arrived
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("processBatch ignored cancellation")
	}
}

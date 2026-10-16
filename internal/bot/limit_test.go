// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"testing"
	"time"

	"github.com/FreshLabDev/voicy/internal/db"
	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/settings"
	"github.com/FreshLabDev/voicy/internal/transcript"
)

const dmVoice200s = `{
  "update_id": 40,
  "message": {
    "message_id": 1,
    "from": {"id": 7, "is_bot": false, "first_name": "A"},
    "chat": {"id": 7, "type": "private"},
    "voice": {"file_id": "LONG1", "file_unique_id": "UL1", "duration": 200}
  }
}`

func TestDailyAllowance(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	usage := []db.BilledAudio{
		{At: ago(20 * time.Hour), Seconds: 1200},
		{At: ago(5 * time.Hour), Seconds: 1800},
		{At: ago(1 * time.Hour), Seconds: 300},
	}
	for _, tc := range []struct {
		name     string
		usage    []db.BilledAudio
		seconds  int
		wantOK   bool
		wantWait time.Duration
	}{
		{name: "nothing used", seconds: 3600, wantOK: true},
		{name: "fits exactly", usage: usage, seconds: 300, wantOK: true},
		// 3300 used + 600 is 300 over: the oldest 1200 s ages out in 4 hours.
		{name: "waits for the oldest", usage: usage, seconds: 600, wantOK: false, wantWait: 4 * time.Hour},
		// 3300 used + 2000 is 1700 over: the oldest two (3000 s) must age out.
		{name: "waits for two", usage: usage, seconds: 2000, wantOK: false, wantWait: 19 * time.Hour},
		// A document without a duration still needs something left.
		{name: "unknown length, allowance spent", usage: []db.BilledAudio{{At: ago(time.Hour), Seconds: 3600}}, seconds: 0, wantOK: false, wantWait: 23 * time.Hour},
		{name: "unknown length, allowance left", usage: usage, seconds: 0, wantOK: true},
		{name: "longer than the whole allowance", seconds: 3601, wantOK: false, wantWait: dailyWindow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wait, ok := dailyAllowance(tc.usage, time.Hour, tc.seconds, now)
			if ok != tc.wantOK || wait != tc.wantWait {
				t.Fatalf("got (%s, %v), want (%s, %v)", wait, ok, tc.wantWait, tc.wantOK)
			}
		})
	}
}

// The refusal has to come before getFile and the download: a person over the
// allowance costs neither Deepgram nor bandwidth.
func TestDailyLimitRefusesBeforeDownloading(t *testing.T) {
	st := &fakeStore{billed: []db.BilledAudio{{At: time.Now().Add(-2 * time.Hour), Seconds: 3500}}}
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "should-not-run"}}
	b := New(st, api, stt, logger())
	b.self = "voicetextbot"
	b.SetDailyAudioLimit(time.Hour)

	if err := b.Handle(context.Background(), parseUpd(t, dmVoice200s)); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 0 || api.gotFile != 0 || api.downloaded != 0 {
		t.Fatalf("over the limit: stt=%d getFile=%d downloaded=%d", stt.calls, api.gotFile, api.downloaded)
	}
	if st.jobs[40] != "limited" {
		t.Fatalf("job status = %q, want limited", st.jobs[40])
	}
	if st.successes != 0 {
		t.Fatal("a refusal must not count as a transcription")
	}
	// The 3500 s were billed two hours ago, so they leave the window in 22 hours.
	want := transcript.DailyLimitText("en", time.Hour, 22*time.Hour)
	got := api.transcriptParts()
	if len(got) != 1 || got[0] != want {
		t.Fatalf("delivered = %q, want %q", got, want)
	}
}

func TestDailyLimitLetsAFittingFileThrough(t *testing.T) {
	st := &fakeStore{billed: []db.BilledAudio{{At: time.Now().Add(-time.Hour), Seconds: 3400}}}
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "fits", Duration: 200}}
	b := New(st, api, stt, logger())
	b.self = "voicetextbot"
	b.SetDailyAudioLimit(time.Hour)

	if err := b.Handle(context.Background(), parseUpd(t, dmVoice200s)); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 1 || st.jobs[40] != "sent" {
		t.Fatalf("stt=%d status=%q", stt.calls, st.jobs[40])
	}
}

// Old usage has left the window and no longer counts.
func TestDailyLimitForgetsUsageOlderThanADay(t *testing.T) {
	st := &fakeStore{billed: []db.BilledAudio{{At: time.Now().Add(-25 * time.Hour), Seconds: 3600}}}
	stt := &countingSTT{res: deepgram.Result{Text: "fresh day"}}
	b := New(st, &fakeTG{}, stt, logger())
	b.self = "voicetextbot"
	b.SetDailyAudioLimit(time.Hour)

	if err := b.Handle(context.Background(), parseUpd(t, dmVoice200s)); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 1 {
		t.Fatalf("stt calls = %d", stt.calls)
	}
}

// A cached transcript costs nothing, so it is served even with the allowance
// spent, and the allowance is not even looked up.
func TestDailyLimitIgnoresCacheHits(t *testing.T) {
	st := &fakeStore{
		cached: map[string]db.Cached{"LONG1\x00" + settings.DefaultVariant: {FileID: "LONG1", Transcript: "cached"}},
		billed: []db.BilledAudio{{At: time.Now().Add(-time.Hour), Seconds: 3600}},
	}
	api := &fakeTG{}
	stt := &countingSTT{}
	b := New(st, api, stt, logger())
	b.self = "voicetextbot"
	b.SetDailyAudioLimit(time.Hour)

	if err := b.Handle(context.Background(), parseUpd(t, dmVoice200s)); err != nil {
		t.Fatal(err)
	}
	if st.billedCalls != 0 {
		t.Fatalf("a cache hit looked up the allowance %d times", st.billedCalls)
	}
	got := api.transcriptParts()
	if len(got) != 1 || !contains(got[0], "cached") || st.jobs[40] != "sent" {
		t.Fatalf("delivered=%v status=%q", got, st.jobs[40])
	}
}

func TestDailyLimitOffByDefault(t *testing.T) {
	st := &fakeStore{billed: []db.BilledAudio{{At: time.Now().Add(-time.Hour), Seconds: 100000}}}
	stt := &countingSTT{res: deepgram.Result{Text: "no limit"}}
	b := New(st, &fakeTG{}, stt, logger())
	b.self = "voicetextbot"

	if err := b.Handle(context.Background(), parseUpd(t, dmVoice200s)); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 1 || st.billedCalls != 0 {
		t.Fatalf("stt=%d billedCalls=%d", stt.calls, st.billedCalls)
	}
}

// /vp is private: the refusal replaces the requester's ephemeral placeholder
// and never reaches the group.
func TestDailyLimitRefusalForVPStaysPrivate(t *testing.T) {
	st := &fakeStore{billed: []db.BilledAudio{{At: time.Now().Add(-time.Hour), Seconds: 3600}}}
	api := &fakeTG{}
	stt := &countingSTT{}
	b := New(st, api, stt, logger())
	b.self = "voicetextbot"
	b.SetDailyAudioLimit(time.Hour)

	upd := parseUpd(t, `{
	  "update_id": 41,
	  "message": {
	    "message_id": 3,
	    "ephemeral_message_id": 88,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/vp",
	    "reply_to_message": {"message_id": 1, "voice": {"file_id": "G1", "file_unique_id": "UG1", "duration": 30}}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 0 || st.jobs[41] != "limited" {
		t.Fatalf("stt=%d status=%q", stt.calls, st.jobs[41])
	}
	if len(api.ephemeralEdits) != 1 || !contains(api.ephemeralEdits[0], "daily limit") {
		t.Fatalf("ephemeral edits = %v", api.ephemeralEdits)
	}
	if len(api.sent) != 0 {
		t.Fatalf("a /vp refusal reached the group: %v", api.sent)
	}
}

// A retried update whose job was already refused stays quiet: the person was
// told once.
func TestDailyLimitRefusalIsNotRepeatedOnRetry(t *testing.T) {
	st := &fakeStore{jobs: map[int64]string{40: "limited"}}
	api := &fakeTG{}
	stt := &countingSTT{}
	b := New(st, api, stt, logger())
	b.self = "voicetextbot"
	b.SetDailyAudioLimit(time.Hour)

	if err := b.Handle(context.Background(), parseUpd(t, dmVoice200s)); err != nil {
		t.Fatal(err)
	}
	if len(api.sent) != 0 || len(api.richEdits) != 0 || stt.calls != 0 {
		t.Fatalf("sent=%v edits=%v stt=%d", api.sent, api.richEdits, stt.calls)
	}
}

// SPDX-License-Identifier: Apache-2.0

// Package metrics exposes a handful of process counters in the Prometheus text
// exposition format. It is deliberately dependency-free: what Voicy needs for
// alerting is a few monotonic counters, not a full client library.
//
// The jobs table already answers "how many transcripts, of what kind, with what
// error code" and survives restarts. These counters cover what never becomes a
// job row: polling failures, Deepgram retries, Telegram rate limits, delivery
// errors. Add a metric with registerCounter or registerVec below; the handler
// renders the registry automatically.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Counter is a monotonically increasing, lock-free metric.
type Counter struct{ n atomic.Int64 }

func (c *Counter) Inc()         { c.n.Add(1) }
func (c *Counter) Add(d int64)  { c.n.Add(d) }
func (c *Counter) value() int64 { return c.n.Load() }

// CounterVec is a counter partitioned by a single label (for example "reason").
// Cells are created lazily on first use.
type CounterVec struct {
	label string
	mu    sync.Mutex
	cells map[string]*atomic.Int64
}

func (c *CounterVec) Inc(labelValue string) {
	c.mu.Lock()
	cell := c.cells[labelValue]
	if cell == nil {
		cell = new(atomic.Int64)
		c.cells[labelValue] = cell
	}
	c.mu.Unlock()
	cell.Add(1)
}

func (c *CounterVec) snapshot() map[string]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int64, len(c.cells))
	for k, v := range c.cells {
		out[k] = v.Load()
	}
	return out
}

type entry struct {
	name    string
	help    string
	counter *Counter
	vec     *CounterVec
}

var registry []entry

func registerCounter(name, help string) *Counter {
	c := &Counter{}
	registry = append(registry, entry{name: name, help: help, counter: c})
	return c
}

func registerVec(name, help, label string) *CounterVec {
	v := &CounterVec{label: label, cells: map[string]*atomic.Int64{}}
	registry = append(registry, entry{name: name, help: help, vec: v})
	return v
}

// Process counters. Names follow Prometheus convention: snake_case with a
// _total suffix for counters.
var (
	UpdatesHandled  = registerCounter("voicy_updates_handled_total", "Telegram updates handled to completion.")
	UpdatesDropped  = registerCounter("voicy_updates_dropped_total", "Telegram updates dropped after exhausting retries.")
	PollFailures    = registerCounter("voicy_poll_failures_total", "getUpdates calls that failed.")
	TranscriptsSent = registerCounter("voicy_transcripts_sent_total", "Transcripts delivered to a user.")
	CacheHits       = registerCounter("voicy_cache_hits_total", "Transcriptions answered from the file_id cache.")
	CacheMisses     = registerCounter("voicy_cache_misses_total", "Transcriptions that had to call Deepgram.")
	EmptyResults    = registerCounter("voicy_empty_results_total", "Deepgram runs that returned no speech.")
	DeepgramRetries = registerCounter("voicy_deepgram_retries_total", "Deepgram requests retried after a transient failure.")
	AudioSeconds    = registerCounter("voicy_audio_seconds_total", "Seconds of audio sent to Deepgram.")
	RedeliverySkips = registerCounter("voicy_redelivery_skips_total", "Retries that skipped delivery because the transcript was already sent.")
	StaleJobsReaped = registerCounter("voicy_stale_jobs_reaped_total", "Jobs failed by the reaper after being abandoned.")

	JobFailures     = registerVec("voicy_job_failures_total", "Transcription jobs that failed, by stage.", "stage")
	TelegramErrors  = registerVec("voicy_telegram_errors_total", "Telegram API errors, by method.", "method")
	TelegramLimited = registerVec("voicy_telegram_rate_limited_total", "Telegram 429 responses observed, by method.", "method")
)

// Handler serves the registry in Prometheus text exposition format.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		Write(w)
	})
}

// Write renders the registry. Exported for tests.
func Write(w io.Writer) {
	for _, e := range registry {
		fmt.Fprintf(w, "# HELP %s %s\n", e.name, e.help)
		fmt.Fprintf(w, "# TYPE %s counter\n", e.name)
		if e.counter != nil {
			fmt.Fprintf(w, "%s %d\n", e.name, e.counter.value())
			continue
		}
		snap := e.vec.snapshot()
		keys := make([]string, 0, len(snap))
		for k := range snap {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "%s{%s=\"%s\"} %d\n", e.name, e.vec.label, escapeLabelValue(k), snap[k])
		}
	}
}

// escapeLabelValue escapes a label value per the Prometheus text format:
// backslash, double quote, and newline. Go's %q is close but not identical.
func escapeLabelValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value)
}

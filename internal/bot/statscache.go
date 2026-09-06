// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"sync"
	"time"

	"github.com/FreshLabDev/voicy/internal/stats"
)

// statsCache serves the statistics panel from memory, mirroring the snapshot
// cache searchy uses. A snapshot is served for ttl; after that the next viewer
// triggers one background refresh while everyone still reads the stale value.
//
// Without it every panel open and every My/All tab switch runs the peak-hour
// histogram over the whole jobs table, so a user toggling tabs is a stream of
// full scans. Key convention: a Telegram user id for personal statistics, 0 for
// the shared global snapshot.
type statsCache struct {
	mu       sync.Mutex
	snaps    map[int64]statsSnap
	inflight map[int64]bool
	ttl      time.Duration
}

type statsSnap struct {
	snap      stats.Snapshot
	expiresAt time.Time
}

func newStatsCache(ttl time.Duration) *statsCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &statsCache{
		snaps:    map[int64]statsSnap{},
		inflight: map[int64]bool{},
		ttl:      ttl,
	}
}

// get returns the cached snapshot even when it has expired, so a stale value can
// be shown while a refresh runs. fresh reports whether it is still within ttl.
func (c *statsCache) get(key int64, now time.Time) (stats.Snapshot, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.snaps[key]
	if !ok {
		return stats.Snapshot{}, false, false
	}
	return entry.snap, now.Before(entry.expiresAt), true
}

func (c *statsCache) put(key int64, snap stats.Snapshot, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snaps[key] = statsSnap{snap: snap, expiresAt: now.Add(c.ttl)}
}

// beginRefresh claims the right to recompute key. It returns false when another
// caller is already refreshing, so concurrent viewers cannot stampede the
// database with the same scan.
func (c *statsCache) beginRefresh(key int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inflight[key] {
		return false
	}
	c.inflight[key] = true
	return true
}

func (c *statsCache) endRefresh(key int64) {
	c.mu.Lock()
	delete(c.inflight, key)
	c.mu.Unlock()
}

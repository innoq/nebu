package ratelimit

// Story 12.12 — Media Gateway Startup + Rate Limiter Hardening (F-4)
//
// Internal (white-box) tests for cleanupOnce to directly verify the race-free
// cleanup behaviour. These tests use `package ratelimit` (not `package ratelimit_test`)
// to access the unexported cleanupOnce method and ipEntry struct.
//
// Acceptance Tests (RED phase — written BEFORE any implementation changes):
//
//   AT-12-12-3 — Cleanup does not delete a recently-accessed entry (within 5m window)
//   AT-12-12-4 — Cleanup deletes a stale entry (outside 5m window)

import (
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// AT-12-12-3 — cleanupOnce does NOT evict an entry accessed 1 second ago.
//
// AC-F4-1 — Given an entry whose lastSeen is 1 second ago (within 5-minute window),
// when cleanupOnce(5 * time.Minute) is called,
// then the entry remains in the sync.Map (not deleted).
//
// This test verifies that a recently-accessed IP (e.g. an in-flight request that
// just called getOrCreate) is not evicted by a concurrent cleanup tick.
func TestIPRateLimiter_CleanupOnce_DoesNotEvictRecentEntry(t *testing.T) {
	t.Parallel()

	rl := &IPRateLimiter{
		rate:  rate.Limit(1),
		burst: 1,
	}

	const ip = "192.0.2.1"
	entry := &ipEntry{
		limiter:  rate.NewLimiter(1, 1),
		lastSeen: time.Now().Add(-1 * time.Second), // 1 second ago — within 5m window
	}
	rl.limiters.Store(ip, entry)

	rl.cleanupOnce(5 * time.Minute)

	if _, ok := rl.limiters.Load(ip); !ok {
		t.Fatal("[AT-12-12-3] recently-accessed entry (1s ago) must NOT be evicted by cleanup with 5m maxAge")
	}
}

// AT-12-12-4 — cleanupOnce EVICTS an entry not seen for 10 minutes.
//
// AC-F4-2 — Given an entry whose lastSeen is 10 minutes ago (outside 5-minute window),
// when cleanupOnce(5 * time.Minute) is called,
// then the entry is deleted from the sync.Map.
//
// This test verifies that stale entries (IPs not seen for >5 minutes) are
// correctly removed to prevent unbounded sync.Map growth.
func TestIPRateLimiter_CleanupOnce_EvictsStaleEntry(t *testing.T) {
	t.Parallel()

	rl := &IPRateLimiter{
		rate:  rate.Limit(1),
		burst: 1,
	}

	const ip = "198.51.100.5"
	entry := &ipEntry{
		limiter:  rate.NewLimiter(1, 1),
		lastSeen: time.Now().Add(-10 * time.Minute), // 10 minutes ago — outside 5m window
	}
	rl.limiters.Store(ip, entry)

	rl.cleanupOnce(5 * time.Minute)

	if _, ok := rl.limiters.Load(ip); ok {
		t.Fatal("[AT-12-12-4] stale entry (10m ago) MUST be evicted by cleanup with 5m maxAge")
	}
}

// AT-12-12-5 — cleanupOnce preserves multiple recent entries and evicts only stale ones.
//
// Mixed scenario: 2 recent + 1 stale entries. Only the stale one is evicted.
func TestIPRateLimiter_CleanupOnce_MixedEntries(t *testing.T) {
	t.Parallel()

	rl := &IPRateLimiter{
		rate:  rate.Limit(1),
		burst: 1,
	}

	recent1 := "10.0.0.1"
	recent2 := "10.0.0.2"
	stale := "10.0.0.3"

	makeEntry := func(age time.Duration) *ipEntry {
		return &ipEntry{
			limiter:  rate.NewLimiter(1, 1),
			lastSeen: time.Now().Add(-age),
		}
	}

	rl.limiters.Store(recent1, makeEntry(30*time.Second))   // 30s — within 5m window
	rl.limiters.Store(recent2, makeEntry(4*time.Minute))    // 4m — within 5m window
	rl.limiters.Store(stale, makeEntry(6*time.Minute))      // 6m — outside 5m window

	rl.cleanupOnce(5 * time.Minute)

	if _, ok := rl.limiters.Load(recent1); !ok {
		t.Errorf("[AT-12-12-5] recent1 (30s ago) must NOT be evicted")
	}
	if _, ok := rl.limiters.Load(recent2); !ok {
		t.Errorf("[AT-12-12-5] recent2 (4m ago) must NOT be evicted")
	}
	if _, ok := rl.limiters.Load(stale); ok {
		t.Errorf("[AT-12-12-5] stale (6m ago) MUST be evicted")
	}
}

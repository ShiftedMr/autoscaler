// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"sync"
	"time"
)

// cooldowns records, per key, a time until which that key should be
// skipped. Zero value is ready to use. Safe for concurrent use, since a
// single provider is shared across every concurrent Create call.
// Read-mostly - every candidate is checked, only failures are recorded -
// so reads take a shared lock.
//
// No eviction: keys are bounded (e.g. len(zones)*len(sizes), or
// regions*families) - tens of entries at most for the life of the process.
type cooldowns[K comparable] struct {
	mu    sync.RWMutex
	until map[K]time.Time
}

// remaining reports how much of k's cooldown is left, if any.
func (c *cooldowns[K]) remaining(k K) (time.Duration, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	remaining := time.Until(c.until[k])
	if remaining <= 0 {
		return 0, false
	}
	return remaining, true
}

// record starts (or extends) k's cooldown, so every other Create call -
// concurrent or subsequent - sees the same signal instead of independently
// rediscovering it. Like recordRateLimit, never shrinks an existing window.
func (c *cooldowns[K]) record(k K, d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	until := time.Now().Add(d)
	if until.After(c.until[k]) {
		if c.until == nil {
			c.until = make(map[K]time.Time)
		}
		c.until[k] = until
	}
}

// regionFamily keys the family-quota cooldown: Google's CPUS_PER_VM_FAMILY
// quota is scoped per (region, machine family), not per (zone, size) - a
// different zone in the same region won't help, but a different family
// (in the same or a different zone) will.
type regionFamily struct{ region, family string }

// familyKey returns the family-quota cooldown key for (zone, size)'s
// machine family in its region.
func familyKey(zone, size string) regionFamily {
	return regionFamily{zoneRegion(zone), machineFamily(size)}
}

// zoneSize keys the stockout cooldown: a stockout
// (ZONE_RESOURCE_POOL_EXHAUSTED) is scoped to one specific (zone, size)
// pair, unlike the family-quota cooldown above - a different zone in the
// same region is unaffected, and a different size in the same zone is
// unaffected too.
type zoneSize struct{ zone, size string }

// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// rateLimitState is the shared, project/region-wide rate-limit signal every
// concurrent caller against one provider can publish into - but
// coordination through it is one-way. Both Create (attemptInZone,
// create.go) and Destroy (via waitOperation, provider.go) can discover a
// rate limit and record it here via recordRateLimit. Only Create's search
// path (rateLimitBackoff, waitForRateLimitTurn) ever reads until or claims
// turn to proactively back off before its own next attempt - Destroy never
// does, so it is not paced or gated by this signal at all. A single
// provider is shared across every concurrent Create/Destroy call, so this
// needs its own synchronization.
type rateLimitState struct {
	// mu guards until. Deliberately separate from turn - a turn is held
	// for an entire attempt (potentially seconds), while mu is only ever
	// held for a brief window check or update; sharing one lock would
	// block every window check behind whoever currently holds the turn.
	mu sync.Mutex
	// until, when in the future, means some call - maybe a different one
	// running concurrently - was rate limited recently enough that every
	// other call should assume the same project/region-wide limit still
	// applies, rather than independently rediscovering it. See
	// rateLimitBackoff/recordRateLimit in create.go.
	until time.Time

	// turn is a weight-1 semaphore used as a binary lock: Acquire(ctx, 1)
	// claims the turn to attempt next, blocking in FIFO order behind any
	// earlier waiter rather than polling, and Release(1) gives it up (see
	// waitForRateLimitTurn/attemptInZone in create.go), often from a
	// different function than the one that claimed it - a semaphore has no
	// notion of "owner" to violate, unlike a mutex. A waiter whose ctx ends
	// while queued is dequeued immediately without blocking whoever's
	// behind it. Unlike mu and until, its zero value is not ready to use -
	// it must be constructed first (see init).
	turn *semaphore.Weighted

	// maxOperationRetries bounds how many times waitOperation retries
	// after a rate limit while polling an operation, before giving up -
	// see the comment there for why this needs its own bound rather than
	// relying on ctx alone (some callers, e.g. the reaper's Destroy path,
	// pass a ctx with no deadline at all).
	maxOperationRetries int

	// maxWaitRetries bounds how many consecutive rate-limit hits a Create
	// call can take without an intervening wait via waitForRateLimitTurn
	// (counted in noteRateLimitHit), before giving up. A call genuinely
	// waiting out repeated rate limits isn't counted - only hits where the
	// shared window read as already clear (e.g. a very short or zero
	// Retry-After), which would otherwise let a call hammer createInZone in
	// a tight, unpaced loop for the rest of its search.
	maxWaitRetries int
}

// init applies defaults and constructs turn. Must be called once, in New,
// before use - unlike mu and until, turn's zero value isn't ready to use.
func (r *rateLimitState) init() {
	if r.maxOperationRetries == 0 {
		r.maxOperationRetries = 5
	}
	if r.maxWaitRetries == 0 {
		r.maxWaitRetries = 5
	}
	r.turn = semaphore.NewWeighted(1)
}

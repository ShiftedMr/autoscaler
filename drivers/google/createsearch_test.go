// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNoteRateLimitHitResetsWhenPaced(t *testing.T) {
	s := &createSearch{
		p:                           &provider{rateLimit: rateLimitState{maxWaitRetries: 5}},
		rateLimitHitsWithoutWaiting: 3,
	}
	rl := &retryAfterError{Err: errors.New("boom"), retryAfter: time.Second}

	if err := s.noteRateLimitHit(context.Background(), "zone", "size", rl, true); err != nil {
		t.Fatalf("expected no error for a paced hit, got %v", err)
	}
	if s.rateLimitHitsWithoutWaiting != 0 {
		t.Errorf("expected the counter to reset after a paced hit, got %d", s.rateLimitHitsWithoutWaiting)
	}
}

func TestNoteRateLimitHitIncrementsWhenUnpaced(t *testing.T) {
	s := &createSearch{p: &provider{rateLimit: rateLimitState{maxWaitRetries: 5}}}
	rl := &retryAfterError{Err: errors.New("boom"), retryAfter: time.Second}

	if err := s.noteRateLimitHit(context.Background(), "zone", "size", rl, false); err != nil {
		t.Fatalf("expected no error below the cap, got %v", err)
	}
	if s.rateLimitHitsWithoutWaiting != 1 {
		t.Errorf("expected the counter to increment, got %d", s.rateLimitHitsWithoutWaiting)
	}
}

func TestNoteRateLimitHitGivesUpAfterCap(t *testing.T) {
	s := &createSearch{
		p:                           &provider{rateLimit: rateLimitState{maxWaitRetries: 2}},
		rateLimitHitsWithoutWaiting: 2,
	}
	rl := &retryAfterError{Err: errors.New("boom"), retryAfter: time.Second}

	err := s.noteRateLimitHit(context.Background(), "zone", "size", rl, false)
	if err == nil {
		t.Fatalf("expected an error once the cap is exceeded")
	}
	if s.rateLimitHitsWithoutWaiting != 3 {
		t.Errorf("expected the counter to still increment on the hit that trips the cap, got %d", s.rateLimitHitsWithoutWaiting)
	}
}

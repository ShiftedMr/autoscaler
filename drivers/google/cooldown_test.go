// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"sync"
	"testing"
	"time"
)

func TestCooldownsZeroValueIsUsable(t *testing.T) {
	var c cooldowns[string]
	if _, cooling := c.remaining("a"); cooling {
		t.Errorf("expected no cooldown in effect on a zero-value cooldowns")
	}
}

func TestCooldownsRemaining(t *testing.T) {
	var c cooldowns[string]

	if _, cooling := c.remaining("a"); cooling {
		t.Errorf("expected no cooldown in effect before any record call")
	}

	c.record("a", time.Minute)

	remaining, cooling := c.remaining("a")
	if !cooling {
		t.Fatalf("expected a cooldown to be in effect right after record")
	}
	if remaining <= 0 || remaining > time.Minute {
		t.Errorf("expected remaining cooldown in (0, 1m], got %v", remaining)
	}
	if _, cooling := c.remaining("b"); cooling {
		t.Errorf("expected an unrelated key to have no cooldown")
	}
}

func TestCooldownsRecordDoesNotShrink(t *testing.T) {
	var c cooldowns[string]

	c.record("a", time.Minute)
	c.record("a", time.Millisecond) // shorter - should not shrink the window

	remaining, cooling := c.remaining("a")
	if !cooling {
		t.Fatalf("expected the cooldown to still be in effect")
	}
	if remaining < 30*time.Second {
		t.Errorf("expected the longer cooldown window to still apply, got remaining=%v", remaining)
	}
}

func TestCooldownsExpire(t *testing.T) {
	var c cooldowns[string]

	c.record("a", time.Millisecond)
	time.Sleep(10 * time.Millisecond)

	if _, cooling := c.remaining("a"); cooling {
		t.Errorf("expected the cooldown to have expired")
	}
}

// TestCooldownsAreRaceFree exercises record/remaining from many goroutines
// concurrently against the same cooldowns - the shape every real Create
// call uses it in, since engine/alloc.go runs one allocation goroutine per
// pending server against a single shared provider. Run with -race to be
// meaningful.
func TestCooldownsAreRaceFree(t *testing.T) {
	var c cooldowns[string]

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.record("a", time.Millisecond)
		}()
		go func() {
			defer wg.Done()
			c.remaining("a")
		}()
	}
	wg.Wait()
}

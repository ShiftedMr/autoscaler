// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drone/autoscaler"
	"github.com/drone/autoscaler/drivers/internal/userdata"
	"github.com/h2non/gock"
)

func TestDefaults(t *testing.T) {
	v, err := New(
		WithClient(http.DefaultClient),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)

	if got, want := p.diskSize, int64(50); got != want {
		t.Errorf("Want diskSize %d, got %d", want, got)
	}
	if got, want := p.diskType, "pd-standard"; got != want {
		t.Errorf("Want diskType %s, got %s", want, got)
	}
	if got, want := p.image, "ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220712"; got != want {
		t.Errorf("Want image %q, got %q", want, got)
	}
	if got, want := p.network, "global/networks/default"; got != want {
		t.Errorf("Want network %q, got %q", want, got)
	}
	if !reflect.DeepEqual(p.scopes, defaultScopes) {
		t.Errorf("Want default scopes")
	}
	if got, want := p.size, "n1-standard-1"; got != want {
		t.Errorf("Want size %q, got %q", want, got)
	}
	if !reflect.DeepEqual(p.tags, defaultTags) {
		t.Errorf("Want default tags")
	}
	if p.userdata != userdata.T {
		t.Errorf("Want default userdata template")
	}
	if p.userdataKey != "user-data" {
		t.Errorf("Want default userdata key")
	}
	if got, want := p.zones, []string{"us-central1-a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Want region %q, got %q", want, got)
	}
}

func TestWaitZoneOperationSuccess(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/op-123").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithProject("my-project"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	err = p.waitZoneOperation(context.Background(), "op-123", "us-central1-a")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestWaitZoneOperationTransientErrorRetry(t *testing.T) {
	defer gock.Off()

	// First call returns 503 Service Unavailable (transient error)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/op-123").
		Reply(503).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    503,
				"message": "Service Unavailable",
			},
		})

	// Subsequent calls succeed
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/op-123").
		Times(5).
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithProject("my-project"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	err = p.waitZoneOperation(context.Background(), "op-123", "us-central1-a")
	if err != nil {
		t.Errorf("Expected no error after retry, got %v", err)
	}
}

func TestWaitZoneOperationNonTransientError(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/op-123").
		Reply(400).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    400,
				"message": "Bad Request",
			},
		})

	v, err := New(
		WithClient(http.DefaultClient),
		WithProject("my-project"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	err = p.waitZoneOperation(context.Background(), "op-123", "us-central1-a")
	if err == nil {
		t.Errorf("Expected error for non-transient error, got nil")
	}
}

func TestWaitZoneOperationNotFound(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/op-123").
		Reply(404).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    404,
				"message": "Not Found",
			},
		})

	v, err := New(
		WithClient(http.DefaultClient),
		WithProject("my-project"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	err = p.waitZoneOperation(context.Background(), "op-123", "us-central1-a")
	if err != autoscaler.ErrInstanceNotFound {
		t.Errorf("Expected ErrInstanceNotFound, got %v", err)
	}
}

func TestWaitGlobalOperationSuccess(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/global/operations/op-123").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithProject("my-project"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	err = p.waitGlobalOperation(context.Background(), "op-123")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestWaitGlobalOperationTransientErrorRetry(t *testing.T) {
	defer gock.Off()

	// First call returns 503 Service Unavailable (transient error)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/global/operations/op-123").
		Reply(503).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    503,
				"message": "Service Unavailable",
			},
		})

	// Subsequent calls succeed
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/global/operations/op-123").
		Times(5).
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithProject("my-project"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	err = p.waitGlobalOperation(context.Background(), "op-123")
	if err != nil {
		t.Errorf("Expected no error after retry, got %v", err)
	}
}

// TestWaitZoneOperationErrorWaitsForDone verifies that an operation which
// already carries an error but is still RUNNING keeps being polled, and the
// error is only returned once the operation is DONE. A failed insert (e.g. a
// stockout) reports its error while Compute Engine is still tearing the
// instance down, and the instance name stays reserved until then - so a
// caller acting on the error early could re-insert the same name and hit a
// 409 alreadyExists.
func TestWaitZoneOperationErrorWaitsForDone(t *testing.T) {
	defer gock.Off()

	opError := map[string]interface{}{
		"errors": []map[string]interface{}{
			{
				"code":    "ZONE_RESOURCE_POOL_EXHAUSTED",
				"message": "The zone does not have enough resources available",
			},
		},
	}
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/op-123").
		Reply(200).
		JSON(map[string]interface{}{"status": "RUNNING", "error": opError})
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/op-123").
		Reply(200).
		JSON(map[string]interface{}{"status": "DONE", "error": opError})

	v, err := New(
		WithClient(http.DefaultClient),
		WithProject("my-project"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	err = p.waitZoneOperation(context.Background(), "op-123", "us-central1-a")
	var opErr *operationError
	if !errors.As(err, &opErr) {
		t.Fatalf("Expected an operationError, got %v", err)
	}
	if got, want := opErr.Code, "ZONE_RESOURCE_POOL_EXHAUSTED"; got != want {
		t.Errorf("Want error code %q, got %q", want, got)
	}
	if pending := len(gock.Pending()); pending != 0 {
		t.Errorf("Expected the operation to be polled until DONE, %d poll(s) never made", pending)
	}
}

func TestWaitZoneOperationRateLimitRetry(t *testing.T) {
	defer gock.Off()

	// First call returns 429 with Retry-After: 0 (retry immediately)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/op-123").
		Reply(429).
		AddHeader("Retry-After", "0").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    429,
				"message": "Too Many Requests",
			},
		})

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/op-123").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithProject("my-project"),
		WithRateLimit(1000),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	err = p.waitZoneOperation(context.Background(), "op-123", "us-central1-a")
	if err != nil {
		t.Errorf("Expected no error after rate limit retry, got %v", err)
	}
}

func TestWaitGlobalOperationNonTransientError(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/global/operations/op-123").
		Reply(400).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    400,
				"message": "Bad Request",
			},
		})

	v, err := New(
		WithClient(http.DefaultClient),
		WithProject("my-project"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	err = p.waitGlobalOperation(context.Background(), "op-123")
	if err == nil {
		t.Errorf("Expected error for non-transient error, got nil")
	}
}

func TestDefaultTimeouts(t *testing.T) {
	v, err := New(
		WithClient(http.DefaultClient),
	)
	if err != nil {
		t.Fatal(err)
	}
	p := v.(*provider)

	if got, want := p.createTimeout, 3*time.Minute; got != want {
		t.Errorf("Want createTimeout %s, got %s", want, got)
	}
	if got, want := p.destroyTimeout, 15*time.Minute; got != want {
		t.Errorf("Want destroyTimeout %s, got %s", want, got)
	}
	if got, want := p.maxTransientDelay, 5*time.Second; got != want {
		t.Errorf("Want maxTransientDelay %s, got %s", want, got)
	}
}

func TestWithCreateTimeoutOverridesDefault(t *testing.T) {
	v, err := New(
		WithClient(http.DefaultClient),
		WithCreateTimeout(45*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.(*provider).createTimeout, 45*time.Second; got != want {
		t.Errorf("Want createTimeout %s, got %s", want, got)
	}
}

func TestNonPositiveCreateTimeoutFallsBackToDefault(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		v, err := New(
			WithClient(http.DefaultClient),
			WithCreateTimeout(d),
		)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := v.(*provider).createTimeout, 3*time.Minute; got != want {
			t.Errorf("Want createTimeout %s for %s, got %s", want, d, got)
		}
	}
}

// TestWaitOperationRecordsRateLimitBeforeGivingUp verifies that a rate
// limit hit while polling an operation extends the shared backoff window
// (rateLimitState.until, from create.go) - not just this call's own retry -
// closing the gap where the shared-window mechanism previously only
// covered the Insert call, not operation polling. Uses a ctx deadline
// short enough that waitUntilFits refuses to wait out the 5s Retry-After,
// so the test doesn't need to actually sleep - it only needs the window
// recorded before that give-up check runs.
func TestWaitOperationRecordsRateLimitBeforeGivingUp(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/op-123").
		Reply(429).
		AddHeader("Retry-After", "5").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    429,
				"message": "Too Many Requests",
			},
		})

	v, err := New(
		WithClient(http.DefaultClient),
		WithProject("my-project"),
		WithRateLimit(1000),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = p.waitZoneOperation(ctx, "op-123", "us-central1-a")
	if err == nil {
		t.Fatalf("expected waitZoneOperation to give up rather than wait past its deadline")
	}
	if _, limited := p.rateLimitBackoff(); !limited {
		t.Errorf("expected the shared rate-limit window to be recorded even though this call gave up waiting for it")
	}
}

// TestWaitOperationGivesUpAfterMaxOperationRateLimitRetries verifies that
// waitOperation doesn't retry a rate limit forever: once
// rateLimitState.maxOperationRetries is exhausted, it gives up instead of
// polling indefinitely.
func TestWaitOperationGivesUpAfterMaxOperationRateLimitRetries(t *testing.T) {
	defer gock.Off()

	const maxRetries = 2

	// The initial attempt plus maxRetries retries, all 429s - exactly this
	// many, so gock.IsDone() below proves waitOperation stopped retrying
	// instead of polling indefinitely. Retry-After is 0 for all but the
	// final (give-up) hit, so the retries' own sleeps stay instant; the
	// final hit gets a real window so the assertion below can tell it was
	// actually recorded, not left over from an earlier hit's already-expired
	// one.
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/op-123").
		Times(maxRetries).
		Reply(429).
		AddHeader("Retry-After", "0").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    429,
				"message": "Too Many Requests",
			},
		})

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/op-123").
		Reply(429).
		AddHeader("Retry-After", "30").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    429,
				"message": "Too Many Requests",
			},
		})

	v, err := New(
		WithClient(http.DefaultClient),
		WithProject("my-project"),
		WithRateLimit(1000),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.rateLimit.maxOperationRetries = maxRetries

	err = p.waitZoneOperation(context.Background(), "op-123", "us-central1-a")
	if err == nil {
		t.Fatalf("expected waitZoneOperation to give up and return an error")
	}
	if !gock.IsDone() {
		t.Errorf("expected exactly maxRetries+1 attempts, got a different count")
	}
	if remaining, limited := p.rateLimitBackoff(); !limited || remaining < 20*time.Second {
		t.Errorf("expected the final hit's ~30s Retry-After to be recorded even though waitOperation gave up on it, got limited=%v remaining=%s", limited, remaining)
	}
}

func TestRateLimitBackoff(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)

	if _, limited := p.rateLimitBackoff(); limited {
		t.Errorf("expected no backoff in effect before any recordRateLimit call")
	}

	p.recordRateLimit(time.Minute)

	remaining, limited := p.rateLimitBackoff()
	if !limited {
		t.Fatalf("expected a backoff to be in effect right after recordRateLimit")
	}
	if remaining <= 0 || remaining > time.Minute {
		t.Errorf("expected remaining backoff in (0, 1m], got %v", remaining)
	}
}

func TestRateLimitBackoffDoesNotShrink(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)

	p.recordRateLimit(time.Minute)
	p.recordRateLimit(time.Millisecond) // shorter - should not shrink the window

	remaining, limited := p.rateLimitBackoff()
	if !limited {
		t.Fatalf("expected the backoff to still be in effect")
	}
	if remaining < 30*time.Second {
		t.Errorf("expected the longer backoff window to still apply, got remaining=%v", remaining)
	}
}

func TestRateLimitBackoffExpires(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)

	p.recordRateLimit(time.Millisecond)
	time.Sleep(10 * time.Millisecond)

	if _, limited := p.rateLimitBackoff(); limited {
		t.Errorf("expected the backoff to have expired")
	}
}

// TestRateLimitTrackingIsRaceFree exercises recordRateLimit/rateLimitBackoff
// from many goroutines concurrently against the same provider - the shape
// every real Create call uses it in, since engine/alloc.go runs one
// allocation goroutine per pending server against a single shared provider.
// Run with -race to be meaningful.
func TestRateLimitTrackingIsRaceFree(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			p.recordRateLimit(time.Millisecond)
		}()
		go func() {
			defer wg.Done()
			p.rateLimitBackoff()
		}()
	}
	wg.Wait()
}

// rateLimitTurnHeld reports whether the shared rate-limit turn is
// currently claimed, without claiming it itself - rateLimit.turn is a
// weight-1 semaphore used directly as the turn-claim primitive (see
// waitForRateLimitTurn in create.go), so tests can't just read a bool;
// TryAcquire then immediately release to peek without side effects.
func (p *provider) rateLimitTurnHeld() bool {
	if p.rateLimit.turn.TryAcquire(1) {
		p.rateLimit.turn.Release(1)
		return false
	}
	return true
}

// TestWaitForRateLimitTurnClaimsImmediatelyWhenClear verifies that, with no
// active backoff window and no other call holding the turn, waitForRateLimitTurn
// returns immediately having claimed it.
func TestWaitForRateLimitTurnClaimsImmediatelyWhenClear(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)

	start := time.Now()
	if err := p.waitForRateLimitTurn(context.Background()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("expected an immediate claim, took %s", elapsed)
	}
	if !p.rateLimitTurnHeld() {
		t.Errorf("expected the turn to be claimed")
	}
}

// TestWaitForRateLimitTurnWaitsOutWindowThenClaims verifies that an active
// backoff window is waited out before the turn is claimed.
func TestWaitForRateLimitTurnWaitsOutWindowThenClaims(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.recordRateLimit(20 * time.Millisecond)

	start := time.Now()
	if err := p.waitForRateLimitTurn(context.Background()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Errorf("expected to have waited out the window (~20ms), only took %s", elapsed)
	}
	if !p.rateLimitTurnHeld() {
		t.Errorf("expected the turn to be claimed after the window cleared")
	}
}

// TestWaitForRateLimitTurnBlocksWhenContended verifies that, if the window
// has already cleared but another call is currently holding the turn,
// waitForRateLimitTurn blocks (rather than failing) until the turn is
// released, then claims it immediately - no polling delay, since the
// semaphore wakes a queued waiter as soon as Release is called.
func TestWaitForRateLimitTurnBlocksWhenContended(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	// Simulates another call already holding the turn, with no active
	// window - otherwise this call would wait out the window first and
	// never reach the contended path this test means to exercise.
	p.rateLimit.turn.Acquire(context.Background(), 1)

	released := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		p.rateLimit.turn.Release(1)
		close(released)
	}()

	start := time.Now()
	err = p.waitForRateLimitTurn(context.Background())
	<-released
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Errorf("expected to have blocked until the turn was released (~20ms), only took %s", elapsed)
	}
	if !p.rateLimitTurnHeld() {
		t.Errorf("expected the turn to be claimed once released")
	}
}

// TestWaitForRateLimitTurnCancelledWaiterDoesNotBlockNext verifies that a
// queued waiter whose ctx ends is dequeued immediately, without blocking a
// subsequent waiter behind it from claiming the turn once it's released.
func TestWaitForRateLimitTurnCancelledWaiterDoesNotBlockNext(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.rateLimit.turn.Acquire(context.Background(), 1)

	// First waiter queues behind the held turn, then has its ctx cancelled
	// before the turn is ever released.
	ctx1, cancel1 := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- p.waitForRateLimitTurn(ctx1)
	}()
	time.Sleep(10 * time.Millisecond) // let the first waiter start queueing
	cancel1()
	if err := <-firstDone; err == nil {
		t.Fatalf("expected the cancelled waiter to return an error")
	}

	// Second waiter queues after the first, and should still successfully
	// claim the turn once released - not wait behind the cancelled one.
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- p.waitForRateLimitTurn(context.Background())
	}()
	time.Sleep(10 * time.Millisecond)
	p.rateLimit.turn.Release(1)

	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("expected the second waiter to claim the turn, got error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("expected the second waiter to claim the turn promptly, it appears blocked")
	}
}

// TestWaitForRateLimitTurnRewaitsWhenWindowExtendedBeforeRelease verifies
// the recheck-after-acquire path: a waiter that queued while the window was
// clear must not attempt the instant the turn is released, because the
// holder it took the turn from extends the window on its way out
// (recordRateLimit runs before the release - see attemptInZone). It should
// hand the turn back and wait the new window out instead of slipping in an
// unpaced attempt against a limit it never looked at.
func TestWaitForRateLimitTurnRewaitsWhenWindowExtendedBeforeRelease(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	// Held with no active window, so the waiter below queues on the
	// semaphore rather than waiting out a window first - otherwise it never
	// reaches the recheck path this test means to exercise.
	p.rateLimit.turn.Acquire(context.Background(), 1)

	const window = 120 * time.Millisecond
	done := make(chan error, 1)
	go func() {
		done <- p.waitForRateLimitTurn(context.Background())
	}()
	time.Sleep(20 * time.Millisecond) // let the waiter queue on the turn

	// Mimics attemptInZone's ordering exactly: record the limit, then
	// release the turn.
	p.recordRateLimit(window)
	released := time.Now()
	p.rateLimit.turn.Release(1)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected the waiter to eventually claim the turn, got error: %v", err)
		}
		if waited := time.Since(released); waited < window/2 {
			t.Errorf("expected the waiter to hand the turn back and wait out the new %s window, but it returned %s after the release", window, waited)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("expected the waiter to return once the new window cleared, it appears blocked")
	}
	if !p.rateLimitTurnHeld() {
		t.Errorf("expected the turn to be claimed once the new window cleared")
	}
}

// TestWaitForRateLimitTurnGivesUpWhenWindowOutlastsDeadline verifies that
// waitForRateLimitTurn fails immediately - without actually waiting - if
// the active window is longer than ctx has left, rather than waiting into
// an opaque context deadline exceeded.
func TestWaitForRateLimitTurnGivesUpWhenWindowOutlastsDeadline(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.recordRateLimit(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = p.waitForRateLimitTurn(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected an error")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected a rate-limited error, got: %v", err)
	}
	if elapsed > 5*time.Millisecond {
		t.Errorf("expected an immediate return rather than waiting for ctx's own deadline, took %s", elapsed)
	}
	if p.rateLimitTurnHeld() {
		t.Errorf("expected no turn to be claimed")
	}
}

// TestWaitForRateLimitTurnRespectsContextCancellation verifies that
// waitForRateLimitTurn returns as soon as ctx is cancelled while waiting
// out an active window, rather than blocking for the full window.
func TestWaitForRateLimitTurnRespectsContextCancellation(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.recordRateLimit(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err = p.waitForRateLimitTurn(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected an error from cancellation")
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("expected to return shortly after cancellation, took %s", elapsed)
	}
}

// TestWaitForRateLimitTurnSerializesMultipleWaiters verifies that, with
// many goroutines waiting for a turn concurrently (the shape engine/alloc.go
// creates - one allocation goroutine per pending server, all against the
// same provider), at most one holds the turn at any given moment. Run with
// -race to be meaningful.
func TestWaitForRateLimitTurnSerializesMultipleWaiters(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)

	var holders int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.waitForRateLimitTurn(context.Background()); err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			n := atomic.AddInt32(&holders, 1)
			if n > 1 {
				t.Errorf("expected at most one concurrent turn holder, got %d", n)
			}
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&holders, -1)
			p.rateLimit.turn.Release(1)
		}()
	}
	wg.Wait()
}

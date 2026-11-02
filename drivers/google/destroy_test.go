// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/drone/autoscaler"
	"github.com/h2non/gock"
)

func TestDestroy(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	mockContext := context.TODO()
	mockInstance := &autoscaler.Instance{
		ID:     "my-instance",
		Region: "us-central1-a",
	}

	p, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
	)
	if err != nil {
		t.Error(err)
		return
	}

	err = p.Destroy(mockContext, mockInstance)
	if err != nil {
		t.Error(err)
	}
}

func TestDestroy_Error(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(404)

	mockContext := context.TODO()
	mockInstance := &autoscaler.Instance{
		ID:     "my-instance",
		Region: "us-central1-a",
	}

	p, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
	)
	if err != nil {
		t.Error(err)
		return
	}

	err = p.Destroy(mockContext, mockInstance)
	if err == nil {
		t.Errorf("Expect error deleting server")
	}
}

func newDestroyTestProvider(t *testing.T) *provider {
	t.Helper()
	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithRateLimit(1000),
	)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*provider)
}

var destroyTestInstance = &autoscaler.Instance{
	ID:     "my-instance",
	Name:   "my-instance",
	Region: "us-central1-a",
}

// TestDestroyGivesUpAfterDestroyTimeout verifies Destroy doesn't poll a
// delete operation forever if it never reaches a terminal state.
func TestDestroyGivesUpAfterDestroyTimeout(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Persist().
		Reply(200).
		BodyString(`{ "status": "RUNNING" }`)

	p := newDestroyTestProvider(t)
	p.destroyTimeout = 50 * time.Millisecond

	err := p.Destroy(context.Background(), destroyTestInstance)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected a context.DeadlineExceeded error once destroyTimeout elapses, got %v", err)
	}
}

// TestDestroyRetriesTransientErrorOnDeleteRequest verifies the delete
// request itself, like Insert, retries a transient 5xx rather than failing
// immediately - safe without Insert's RequestId protection against duplicate
// creation, since a repeat Delete on an instance already gone just 404s.
func TestDestroyRetriesTransientErrorOnDeleteRequest(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(503).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{"code": 503, "message": "Service Unavailable"},
		})

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	p := newDestroyTestProvider(t)

	if err := p.Destroy(context.Background(), destroyTestInstance); err != nil {
		t.Fatalf("expected Destroy to succeed after retrying the transient 503, got %v", err)
	}
	if !gock.IsDone() {
		t.Errorf("expected both delete attempts and the operation poll to be consumed")
	}
}

// TestDestroyReturnsInstanceNotFoundWhenAlreadyGone verifies the 404 mapping
// survives the timeout wrapper, since the engine treats it as success rather
// than an error to retry.
func TestDestroyReturnsInstanceNotFoundWhenAlreadyGone(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(404)

	p := newDestroyTestProvider(t)

	if err := p.Destroy(context.Background(), destroyTestInstance); err != autoscaler.ErrInstanceNotFound {
		t.Fatalf("expected ErrInstanceNotFound, got %v", err)
	}
}

// TestDestroyReturnsInstanceNotFoundWhenPollReturns404 verifies the 404
// mapping also applies to the operation poll, not just the initial delete
// request - e.g. a reaper retry racing an earlier attempt whose delete
// already completed, so ZoneOperations.Get 404s on the (now-gone)
// operation name.
func TestDestroyReturnsInstanceNotFoundWhenPollReturns404(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(404)

	p := newDestroyTestProvider(t)

	if err := p.Destroy(context.Background(), destroyTestInstance); err != autoscaler.ErrInstanceNotFound {
		t.Fatalf("expected ErrInstanceNotFound, got %v", err)
	}
}

// TestDestroyStopsPollingWhenParentContextIsCancelled verifies that if the
// caller's own ctx is cancelled (e.g. engine shutdown, which cancels the
// root ctx the reaper passes - see engine/reaper.go) while Destroy is
// polling, Destroy returns promptly rather than continuing until
// destroyTimeout elapses.
func TestDestroyStopsPollingWhenParentContextIsCancelled(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Persist().
		Reply(200).
		BodyString(`{ "status": "RUNNING" }`)

	p := newDestroyTestProvider(t)
	// Long enough that only the cancellation, not destroyTimeout, should
	// stop this.
	p.destroyTimeout = time.Minute

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := p.Destroy(ctx, destroyTestInstance)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled once the parent ctx is cancelled, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("expected Destroy to stop promptly after cancellation, took %s", elapsed)
	}
}

// TestDestroyReturnsOperationError verifies a definitive operation failure
// (Google rejected the delete) is returned as an *operationError carrying
// the operation's code, so the caller can tell it apart from a timeout.
func TestDestroyReturnsOperationError(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		JSON(map[string]interface{}{
			"status": "DONE",
			"error": map[string]interface{}{
				"errors": []map[string]interface{}{
					{
						"code":    "RESOURCE_OPERATION_RATE_EXCEEDED",
						"message": "delete rejected",
					},
				},
			},
		})

	p := newDestroyTestProvider(t)

	var opErr *operationError
	if err := p.Destroy(context.Background(), destroyTestInstance); !errors.As(err, &opErr) {
		t.Fatalf("expected Destroy to return the operation error, got %v", err)
	}
	if got, want := opErr.Code, "RESOURCE_OPERATION_RATE_EXCEEDED"; got != want {
		t.Errorf("Want operation error code %q, got %q", want, got)
	}
}

// TestDestroyTimeoutAppliesToTheDeleteRequestItself verifies destroyTimeout
// also covers asking Google to delete, not just waiting on the operation.
// The error is returned for the reaper to retry.
func TestDestroyTimeoutAppliesToTheDeleteRequestItself(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(200).
		Delay(2 * time.Second).
		BodyString(`{ "name": "operation-name" }`)

	p := newDestroyTestProvider(t)
	p.destroyTimeout = 50 * time.Millisecond

	start := time.Now()
	err := p.Destroy(context.Background(), destroyTestInstance)
	if err == nil {
		t.Fatalf("expected an error when the delete request outlasts destroyTimeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("expected Destroy to give up near destroyTimeout (50ms), took %s", elapsed)
	}
}

// TestDestroyRateLimitedPollGivesUpAfterMaxRetries verifies that a delete
// whose operation poll is rate limited past maxOperationRetries returns an
// error wrapping the rate limit, leaving the server for the reaper to
// retry.
func TestDestroyRateLimitedPollGivesUpAfterMaxRetries(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	// The initial poll plus one retry, then give up - exactly this many.
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Times(2).
		Reply(429).
		AddHeader("Retry-After", "0").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{"code": 429, "message": "Too Many Requests"},
		})

	p := newDestroyTestProvider(t)
	p.rateLimit.maxOperationRetries = 1

	err := p.Destroy(context.Background(), destroyTestInstance)
	var rl *retryAfterError
	if !errors.As(err, &rl) {
		t.Fatalf("expected the give-up error to wrap the rate limit, got %v", err)
	}
	if !gock.IsDone() {
		t.Errorf("expected exactly the initial poll plus one retry")
	}
}

// TestDestroyPublishesRateLimitIntoSharedWindow pins that a rate limit
// Destroy hits while polling is recorded for every other caller to see -
// intentional, since the quota is shared across Create and Destroy.
func TestDestroyPublishesRateLimitIntoSharedWindow(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(429).
		AddHeader("Retry-After", "30").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{"code": 429, "message": "Too Many Requests"},
		})

	p := newDestroyTestProvider(t)
	// Short enough that waiting out the 30s Retry-After can't fit, so
	// Destroy gives up immediately instead of sleeping - after recording.
	p.destroyTimeout = 100 * time.Millisecond

	if err := p.Destroy(context.Background(), destroyTestInstance); err == nil {
		t.Fatalf("expected Destroy to give up rather than wait out the Retry-After")
	}
	if remaining, limited := p.rateLimitBackoff(); !limited || remaining < 20*time.Second {
		t.Errorf("expected Destroy to publish the ~30s rate limit into the shared window, got limited=%v remaining=%s", limited, remaining)
	}
}

// TestDestroyPublishesRateLimitFromDeleteRequestIntoSharedWindow verifies a
// 429 on the delete request itself, not just the operation poll, is
// recorded into the shared window, so a concurrent Create call can see it
// too.
func TestDestroyPublishesRateLimitFromDeleteRequestIntoSharedWindow(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/my-instance").
		Reply(429).
		AddHeader("Retry-After", "30").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{"code": 429, "message": "Too Many Requests"},
		})

	p := newDestroyTestProvider(t)

	var rl *retryAfterError
	if err := p.Destroy(context.Background(), destroyTestInstance); !errors.As(err, &rl) {
		t.Fatalf("expected Destroy to return an error wrapping the rate limit, got %v", err)
	}
	if remaining, limited := p.rateLimitBackoff(); !limited || remaining < 20*time.Second {
		t.Errorf("expected Destroy to publish the ~30s rate limit from the delete request into the shared window, got limited=%v remaining=%s", limited, remaining)
	}
}

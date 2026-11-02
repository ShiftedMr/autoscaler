// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drone/autoscaler"
	"github.com/drone/autoscaler/logger"
	"github.com/h2non/gock"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
)

func TestCreate(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		JSON(insertInstanceMock).
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Error(err)
	}

	if want, got := instance.Address, "1.2.3.4"; got != want {
		t.Errorf("Want instance IP %q, got %q", want, got)
	}
	if want, got := instance.Image, "ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220712"; got != want {
		t.Errorf("Want instance ID %q, got %q", want, got)
	}
	if want, got := instance.ID, "agent-807jvfwj"; got != want {
		t.Errorf("Want instance ID %q, got %q", want, got)
	}
	if want, got := instance.Name, "agent-807jVFwj"; got != want {
		t.Errorf("Want instance Name %q, got %q", want, got)
	}
	if want, got := instance.Provider, autoscaler.ProviderGoogle; got != want {
		t.Errorf("Want google Provider type")
	}
	if want, got := instance.Region, "us-central1-a"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
	if want, got := instance.Size, "n1-standard-1"; got != want {
		t.Errorf("Want instance Size %q, got %q", want, got)
	}
	if want, got := instance.ServiceAccountEmail, "default"; got != want {
		t.Errorf("Want service account email  %q, got %q", want, got)
	}
}

func TestCreateWithMultiZones(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		JSON(insertInstanceMockB).
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-b/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-b/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Error(err)
	}

	if want, got := instance.Region, "us-central1-b"; got != want {
		t.Errorf("Want region %q, got %q", want, got)
	}
}

var insertInstanceMock = &compute.Instance{
	Name:           "agent-807jvfwj",
	Zone:           "projects/my-project/zones/us-central1-a",
	MinCpuPlatform: "Automatic",
	MachineType:    "projects/my-project/zones/us-central1-a/machineTypes/n1-standard-1",
	Metadata: &compute.Metadata{
		Items: []*compute.MetadataItems{
			{
				Key:   "user-data",
				Value: googleapi.String(`#cloud-init`),
			},
		},
	},
	Tags: &compute.Tags{
		Items: []string{"allow-docker"},
	},
	Disks: []*compute.AttachedDisk{
		{
			Type:       "PERSISTENT",
			Boot:       true,
			Mode:       "READ_WRITE",
			AutoDelete: true,
			DeviceName: "agent-807jvfwj",
			InitializeParams: &compute.AttachedDiskInitializeParams{
				SourceImage: "https://www.googleapis.com/compute/v1/projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220712",
				DiskType:    "projects/my-project/zones/us-central1-a/diskTypes/pd-standard",
				DiskSizeGb:  50,
			},
		},
	},
	CanIpForward: false,
	NetworkInterfaces: []*compute.NetworkInterface{
		{
			Network:   "global/networks/default",
			StackType: "IPV4_ONLY",
			AccessConfigs: []*compute.AccessConfig{
				{
					Name: "External NAT",
					Type: "ONE_TO_ONE_NAT",
				},
			},
		},
	},
	Labels: map[string]string{},
	Scheduling: &compute.Scheduling{
		Preemptible:       false,
		OnHostMaintenance: "MIGRATE",
		AutomaticRestart:  googleapi.Bool(true),
	},
	DeletionProtection: false,
	ServiceAccounts: []*compute.ServiceAccount{
		{
			Email: "default",
			Scopes: []string{
				"https://www.googleapis.com/auth/devstorage.read_only",
				"https://www.googleapis.com/auth/logging.write",
				"https://www.googleapis.com/auth/monitoring.write",
				"https://www.googleapis.com/auth/trace.append",
			},
		},
	},
}

var insertInstanceMockB = &compute.Instance{
	Name:           "agent-807jvfwj",
	Zone:           "projects/my-project/zones/us-central1-b",
	MinCpuPlatform: "Automatic",
	MachineType:    "projects/my-project/zones/us-central1-b/machineTypes/n1-standard-1",
	Metadata: &compute.Metadata{
		Items: []*compute.MetadataItems{
			{
				Key:   "user-data",
				Value: googleapi.String(`#cloud-init`),
			},
		},
	},
	Tags: &compute.Tags{
		Items: []string{"allow-docker"},
	},
	Disks: []*compute.AttachedDisk{
		{
			Type:       "PERSISTENT",
			Boot:       true,
			Mode:       "READ_WRITE",
			AutoDelete: true,
			DeviceName: "agent-807jvfwj",
			InitializeParams: &compute.AttachedDiskInitializeParams{
				SourceImage: "https://www.googleapis.com/compute/v1/projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220712",
				DiskType:    "projects/my-project/zones/us-central1-b/diskTypes/pd-standard",
				DiskSizeGb:  50,
			},
		},
	},
	CanIpForward: false,
	NetworkInterfaces: []*compute.NetworkInterface{
		{
			Network:   "global/networks/default",
			StackType: "IPV4_ONLY",
			AccessConfigs: []*compute.AccessConfig{
				{
					Name: "External NAT",
					Type: "ONE_TO_ONE_NAT",
				},
			},
		},
	},
	Labels: map[string]string{},
	Scheduling: &compute.Scheduling{
		Preemptible:       false,
		OnHostMaintenance: "MIGRATE",
		AutomaticRestart:  googleapi.Bool(true),
	},
	DeletionProtection: false,
	ServiceAccounts: []*compute.ServiceAccount{
		{
			Email: "default",
			Scopes: []string{
				"https://www.googleapis.com/auth/devstorage.read_only",
				"https://www.googleapis.com/auth/logging.write",
				"https://www.googleapis.com/auth/monitoring.write",
				"https://www.googleapis.com/auth/trace.append",
			},
		},
	},
}

func TestCreateWithZoneOperationTransientError(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		JSON(insertInstanceMock).
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	// First call returns 503 Service Unavailable (transient error)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(503).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    503,
				"message": "Service Unavailable",
			},
		})

	// Subsequent calls succeed (operation polling retries)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Times(5).
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Error(err)
	}

	if want, got := instance.Address, "1.2.3.4"; got != want {
		t.Errorf("Want instance IP %q, got %q", want, got)
	}
	if want, got := instance.ID, "agent-807jvfwj"; got != want {
		t.Errorf("Want instance ID %q, got %q", want, got)
	}
}

func TestCreateWithInsertTransientError(t *testing.T) {
	defer gock.Off()

	// First Insert call returns 503 Service Unavailable (transient error)
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		JSON(insertInstanceMock).
		Reply(503).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    503,
				"message": "Service Unavailable",
			},
		})

	// Subsequent Insert calls succeed (Insert retry logic)
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		JSON(insertInstanceMock).
		Times(5).
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	// Operation polling succeeds
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Error(err)
	}

	if want, got := instance.Address, "1.2.3.4"; got != want {
		t.Errorf("Want instance IP %q, got %q", want, got)
	}
	if want, got := instance.ID, "agent-807jvfwj"; got != want {
		t.Errorf("Want instance ID %q, got %q", want, got)
	}
}

func newCreateTestProvider(t *testing.T) *provider {
	t.Helper()
	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Fatal(err)
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.maxTransientDelay = time.Millisecond
	return p
}

func mockAcceptedInsert() {
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)
}

// TestCreateReturnsInstanceWhenOperationPollTimesOut verifies that once
// Google has accepted the insert, a poll that never reaches DONE before
// createTimeout does not drop the VM: Create returns its identity alongside
// the error, so the engine records it and the reaper can destroy it instead
// of leaving it running untracked.
func TestCreateReturnsInstanceWhenOperationPollTimesOut(t *testing.T) {
	defer gock.Off()

	mockAcceptedInsert()
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Persist().
		Reply(200).
		BodyString(`{ "status": "RUNNING" }`)

	p := newCreateTestProvider(t)
	p.createTimeout = 100 * time.Millisecond

	instance, err := p.Create(context.Background(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})

	var unconfirmed *insertUnconfirmedError
	if !errors.As(err, &unconfirmed) {
		t.Fatalf("expected an *insertUnconfirmedError, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected the error to wrap context.DeadlineExceeded, got %v", err)
	}
	if instance == nil {
		t.Fatal("expected the accepted instance to be returned alongside the error")
	}
	if want, got := "agent-807jvfwj", instance.ID; got != want {
		t.Errorf("Want instance ID %q, got %q", want, got)
	}
	if want, got := "us-central1-a", instance.Region; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
}

// TestCreateReturnsInstanceOnGetFailure verifies the same
// when the operation finished but fetching the instance failed.
func TestCreateReturnsInstanceOnGetFailure(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(403).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{"code": 403, "message": "forbidden"},
		})

	p := newUnconfirmedInsertTestProvider(t, "us-central1-a")

	instance, err := p.Create(context.Background(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	var unconfirmed *insertUnconfirmedError
	if !errors.As(err, &unconfirmed) {
		t.Fatalf("expected an *insertUnconfirmedError, got %v", err)
	}
	if instance == nil || instance.ID != "agent-807jvfwj" {
		t.Fatalf("expected the instance's identity to be returned, got %v", instance)
	}
}

// TestCreateReturnsNoInstanceOnOperationError verifies a definitive failure -
// the operation reached DONE with an error - returns no instance: nothing
// was created, so there is nothing for the reaper to clean up.
func TestCreateReturnsNoInstanceOnOperationError(t *testing.T) {
	defer gock.Off()

	mockAcceptedInsert()
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		JSON(map[string]interface{}{
			"status": "DONE",
			"error": map[string]interface{}{
				"errors": []map[string]interface{}{
					{
						"code":    "ZONE_RESOURCE_POOL_EXHAUSTED",
						"message": "The zone does not have enough resources available",
					},
				},
			},
		})

	p := newCreateTestProvider(t)

	instance, err := p.Create(context.Background(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})

	var opErr *operationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected an *operationError, got %v", err)
	}
	if got, want := opErr.Code, "ZONE_RESOURCE_POOL_EXHAUSTED"; got != want {
		t.Errorf("Want error code %q, got %q", want, got)
	}
	var unconfirmed *insertUnconfirmedError
	if errors.As(err, &unconfirmed) {
		t.Errorf("a definitive operation error is not an unconfirmed insert")
	}
	if instance != nil {
		t.Errorf("expected no instance when the operation failed, got %+v", instance)
	}
}

// TestCreateReturnsNoInstanceWhenInsertRejected verifies an insert Google
// rejects outright returns no instance.
func TestCreateReturnsNoInstanceWhenInsertRejected(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(400).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{"code": 400, "message": "Invalid value for field 'machineType'"},
		})

	p := newCreateTestProvider(t)

	instance, err := p.Create(context.Background(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err == nil {
		t.Fatal("expected an error")
	}
	var unconfirmed *insertUnconfirmedError
	if errors.As(err, &unconfirmed) {
		t.Errorf("a rejected insert is not an unconfirmed insert")
	}
	if instance != nil {
		t.Errorf("expected no instance, got %+v", instance)
	}
}

// TestCreateSendsNothingWhenContextAlreadyExpired verifies that a ctx that
// has already ended means no insert was sent, so there is nothing for the
// reaper to clean up and no instance is returned.
func TestCreateSendsNothingWhenContextAlreadyExpired(t *testing.T) {
	defer gock.Off()

	// Never consumed: an insert must not be sent.
	insert := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	p := newCreateTestProvider(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	instance, err := p.Create(ctx, autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	var unconfirmed *insertUnconfirmedError
	if errors.As(err, &unconfirmed) {
		t.Errorf("an insert that was never sent is not an unconfirmed insert")
	}
	if instance != nil {
		t.Errorf("expected no instance, got %+v", instance)
	}
	if insert.Done() {
		t.Errorf("expected no insert request to be sent")
	}
}

// TestCreateRetriesSameZoneOnRateLimit verifies that a 429 causes Create to
// wait out the Retry-After duration and retry the same zone, rather than
// immediately moving on to a different candidate (moving on wouldn't avoid
// a project-level rate limit, and would only add more load while throttled).
// TestCreateWaitsOutRateLimitAsSoleWaiter verifies that, as the only Create
// call currently running, a 429 is waited out and retried against the same
// zone (succeeding once it clears) rather than moving on to another
// candidate: every candidate shares the same project/region-wide limit, so
// trying another one wouldn't avoid it and would only add more load to an
// already rate-limited quota.
func TestCreateWaitsOutRateLimitAsSoleWaiter(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(429).
		AddHeader("Retry-After", "0").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    429,
				"message": "Too Many Requests",
			},
		})

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	// Registered so that, if Create wrongly moves on to another candidate
	// instead of retrying us-central1-a, the test fails clearly via
	// untried.Done() below rather than by accidentally succeeding through
	// a different zone.
	untried := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Reply(200).
		JSON(map[string]interface{}{"name": "operation-name"})

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.shuffleZones = noShuffle

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected the sole waiter to succeed after retrying, got error: %v", err)
	}
	if want, got := instance.Region, "us-central1-a"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
	if untried.Done() {
		t.Errorf("expected us-central1-b never to be attempted")
	}
	if p.rateLimitTurnHeld() {
		t.Errorf("expected the wait claim to be released after succeeding")
	}
}

// TestCreateReleasesRateLimitWaitOnPanic verifies that a panic inside
// createInZone - while this call is holding the rate-limit turn - still
// releases it, rather than leaving the turn stuck claimed and wedging
// every other Create call on this provider that ever needs to wait for one
// again. Triggers a real panic via the (pre-existing, unrelated to this
// mechanism) unchecked NetworkInterfaces[0] indexing in createInZone, by
// having instances.Get return no network interfaces at all.
//
// Uses a real (non-zero) Retry-After so the retry actually goes through
// waitForRateLimitTurn and claims the turn before the panicking attempt -
// a "Retry-After: 0" would leave the window already expired by the very
// next check, so the claim path (and thus the bug this test guards) would
// never be exercised at all.
func TestCreateReleasesRateLimitWaitOnPanic(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(429).
		AddHeader("Retry-After", "1").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    429,
				"message": "Too Many Requests",
			},
		})

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	panicked := func() (didPanic bool) {
		defer func() {
			if recover() != nil {
				didPanic = true
			}
		}()
		p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
		return false
	}()

	if !panicked {
		t.Fatalf("expected createInZone's empty NetworkInterfaces indexing to panic")
	}
	if p.rateLimitTurnHeld() {
		t.Errorf("expected the wait claim to be released even though createInZone panicked")
	}
}

// TestCreateWaitsOutWriteQuotaExceededAsSoleWaiter is the same as
// TestCreateWaitsOutRateLimitAsSoleWaiter but for the 403 write-quota-
// exceeded shape Compute actually returns for an exhausted
// per-project-per-region quota (seen in production logs), which
// isRateLimitError also recognizes.
func TestCreateWaitsOutWriteQuotaExceededAsSoleWaiter(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(403).
		AddHeader("Retry-After", "0").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    403,
				"message": "Quota exceeded for quota metric 'Write requests' and limit 'Write requests per minute per region' of service 'compute.googleapis.com' for consumer 'project_number:635500232573'.",
				"errors": []map[string]interface{}{
					{
						"reason":  "rateLimitExceeded",
						"message": "Quota exceeded for quota metric 'Write requests' and limit 'Write requests per minute per region' of service 'compute.googleapis.com' for consumer 'project_number:635500232573'.",
					},
				},
			},
		})

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	untried := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Reply(200).
		JSON(map[string]interface{}{"name": "operation-name"})

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.shuffleZones = noShuffle

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected the sole waiter to succeed after retrying, got error: %v", err)
	}
	if want, got := instance.Region, "us-central1-a"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
	if untried.Done() {
		t.Errorf("expected us-central1-b never to be attempted")
	}
}

// TestCreateGivesUpWaitingWhenItWouldOutlastTheSearch verifies that the
// sole waiter doesn't start a wait it can't finish: if the shared backoff
// window is longer than createSearchTimeout has left, it abandons the
// search immediately with a clear "rate limited" error instead of waiting
// into an opaque context deadline exceeded.
func TestCreateGivesUpWaitingWhenItWouldOutlastTheSearch(t *testing.T) {
	defer gock.Off()

	// Only one attempt should ever happen: Create must give up as soon as
	// it sees the wait is unwinnable, not retry after waiting anyway.
	// gock.IsDone() below fails clearly if a second attempt happened
	// instead (the default matcher only matches once).
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(429).
		AddHeader("Retry-After", "3600").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    429,
				"message": "Too Many Requests",
			},
		})

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	// Far shorter than the 1-hour Retry-After above, so the wait is
	// guaranteed to be judged as longer than the search has left.
	p.createSearchTimeout = time.Second

	start := time.Now()
	_, err = p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected Create to give up and return an error")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected a rate-limited error, got: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected an immediate return with no wait since the backoff outlasts the search, took %s", elapsed)
	}
	if !gock.IsDone() {
		t.Errorf("expected exactly one attempt - no retry once the wait is judged unwinnable")
	}
	if p.rateLimitTurnHeld() {
		t.Errorf("expected the wait claim to be released after giving up")
	}
}

// TestCreateGivesUpWaitingAfterMaxRateLimitWaitRetries verifies that the
// sole waiter doesn't wait forever: once rateLimitState.maxWaitRetries is
// exhausted, it abandons the search like any other call would instead of
// continuing to retry.
func TestCreateGivesUpWaitingAfterMaxRateLimitWaitRetries(t *testing.T) {
	defer gock.Off()

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	// The initial attempt plus rateLimitState.maxWaitRetries retries, all 429s -
	// exactly this many, so gock.IsDone() below proves Create stopped
	// retrying instead of waiting indefinitely.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Times(p.rateLimit.maxWaitRetries+1).
		Reply(429).
		AddHeader("Retry-After", "0").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    429,
				"message": "Too Many Requests",
			},
		})

	_, err = p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err == nil {
		t.Fatalf("expected Create to give up and return an error")
	}
	if !gock.IsDone() {
		t.Errorf("expected exactly rateLimitState.maxWaitRetries+1 attempts, got a different count")
	}
	if p.rateLimitTurnHeld() {
		t.Errorf("expected the wait claim to be released after giving up")
	}
}

// TestCreateDoesNotGiveUpAfterManyPacedRateLimitHits verifies that a call
// genuinely waiting out real, repeated rate limits - each one properly
// paced via waitForRateLimitTurn - doesn't give up early just because the
// total hit count exceeds rateLimitState.maxWaitRetries: that cap only counts
// consecutive hits without a wait in between (see the comment on it), not
// every hit outright. Uses a real Retry-After (parseRetryAfter only parses
// whole seconds) so each hit is a genuine paced wait, not an instant
// bypass of waitForRateLimitTurn.
func TestCreateDoesNotGiveUpAfterManyPacedRateLimitHits(t *testing.T) {
	defer gock.Off()

	// 3 paced hits - more than rateLimitState.maxWaitRetries(2) - then success.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Times(3).
		Reply(429).
		AddHeader("Retry-After", "1").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    429,
				"message": "Too Many Requests",
			},
		})

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.rateLimit.maxWaitRetries = 2

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected Create to succeed despite 3 hits exceeding rateLimitState.maxWaitRetries(2), since each was properly paced, got error: %v", err)
	}
	if want, got := instance.Region, "us-central1-a"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
	if !gock.IsDone() {
		t.Errorf("expected every registered mock to be consumed")
	}
}

// TestCreateSkipsEntirelyWhenAlreadyRateLimited verifies that, if the
// shared backoff window is already in effect (set by a concurrent or
// preceding Create call), a new Create call fails immediately without
// attempting any API call at all.
func TestCreateSkipsEntirelyWhenAlreadyRateLimited(t *testing.T) {
	defer gock.Off()

	// Registered so that, if Create wrongly proceeds without waiting out
	// the pre-existing window at all, the test fails clearly via
	// untried.Done() below rather than by accidentally succeeding early.
	untried := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		JSON(map[string]interface{}{"name": "operation-name"})

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.createSearchTimeout = 10 * time.Millisecond
	p.recordRateLimit(time.Hour)

	start := time.Now()
	_, err = p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected Create to give up and return an error")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected a rate-limited error, got: %v", err)
	}
	if untried.Done() {
		t.Errorf("expected no Insert call at all - the pre-existing window already outlasts the search")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected an immediate return since the wait is judged unwinnable up front, took %s", elapsed)
	}
}

// TestCreateWaitsItsTurnWhenAlreadyRateLimitedAtStart verifies that, if the
// shared backoff window is already in effect when Create starts (set by a
// concurrent or preceding call) but is short enough to fit within
// createTimeout, Create waits it out and still attempts normally
// once it clears - rather than failing immediately just because a window
// happened to already be active.
func TestCreateWaitsItsTurnWhenAlreadyRateLimitedAtStart(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.recordRateLimit(10 * time.Millisecond)

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected Create to wait out the short window and succeed, got error: %v", err)
	}
	if want, got := instance.Region, "us-central1-a"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
}

// TestCreateReturnsInstanceOnPollRateLimitGiveUp verifies that
// when an accepted Insert's operation poll exhausts its rate-limit retries,
// Create neither re-inserts the name nor drops the VM: it stops and returns
// the instance's identity with the error, so the reaper can destroy it.
func TestCreateReturnsInstanceOnPollRateLimitGiveUp(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Times(1).
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	// No second Insert for the name, in this zone or the next.
	reinsertA := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(409).
		JSON(alreadyExistsResponse("us-central1-a", "agent-807jvfwj"))
	reinsertB := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	// The initial poll plus one retry, then give up - exactly this many.
	rateLimitedPollMock("us-central1-a", 2)

	// Cleanup belongs to the reaper, not Create.
	del := gock.New("https://compute.googleapis.com").
		Delete("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "name": "delete-operation-name" }`)

	p := newUnconfirmedInsertTestProvider(t, "us-central1-a", "us-central1-b")
	p.shuffleZones = noShuffle
	p.rateLimit.maxOperationRetries = 1

	instance, err := p.Create(context.Background(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	var unconfirmed *insertUnconfirmedError
	if !errors.As(err, &unconfirmed) {
		t.Fatalf("expected an *insertUnconfirmedError, got %v", err)
	}
	if instance == nil {
		t.Fatalf("expected Create to return the instance's identity alongside the error")
	}
	if want, got := instance.ID, "agent-807jvfwj"; got != want {
		t.Errorf("Want instance ID %q, got %q", want, got)
	}
	if want, got := instance.Region, "us-central1-a"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
	if reinsertA.Done() || reinsertB.Done() {
		t.Errorf("expected the accepted Insert not to be re-issued in any zone")
	}
	if del.Done() {
		t.Errorf("did not expect Create to delete the instance itself")
	}
}

// TestCreateLogsOperationErrorCode verifies that when a zone operation fails
// with a structured Code (e.g. QUOTA_EXCEEDED), that code is still visible
// in the log even though operationError.Error() only renders Message.
func TestCreateLogsOperationErrorCode(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		JSON(insertInstanceMock).
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
						"code":    "QUOTA_EXCEEDED",
						"message": "Quota 'CPUS' exceeded. Limit: 24.0 in region us-central1.",
					},
				},
			},
		})

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	logrusLogger, hook := logrustest.NewNullLogger()
	ctx := logger.WithContext(context.TODO(), logger.Logrus(logrus.NewEntry(logrusLogger)))

	_, err = p.Create(ctx, autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err == nil {
		t.Fatalf("expected an error from the failed operation")
	}

	var found *logrus.Entry
	for _, entry := range hook.AllEntries() {
		if _, ok := entry.Data["code"]; ok {
			found = entry
			break
		}
	}
	if found == nil {
		t.Fatalf("expected the structured operation error code in a log entry, got entries: %v", hook.AllEntries())
	}
	if got, want := found.Data["code"], "QUOTA_EXCEEDED"; got != want {
		t.Errorf("Want log field code %q, got %q", want, got)
	}
	if got, want := found.Data["image"], "ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220712"; got != want {
		t.Errorf("Want log field image %q, got %q", want, got)
	}
	if got, want := found.Data["diskType"], "pd-standard"; got != want {
		t.Errorf("Want log field diskType %q, got %q", want, got)
	}
	if got, want := found.Data["name"], "agent-807jVFwj"; got != want {
		t.Errorf("Want log field name %q, got %q", want, got)
	}
	if found.Level != logrus.ErrorLevel {
		t.Errorf("expected an unrecognized operation error to log at Error level, got %s", found.Level)
	}
}

func stockoutResponse() map[string]interface{} {
	return map[string]interface{}{
		"error": map[string]interface{}{
			"code": 400,
			"errors": []map[string]interface{}{
				{"reason": "ZONE_RESOURCE_POOL_EXHAUSTED"},
			},
			"message": "The zone does not have enough resources available",
		},
	}
}

func TestCreateWithZoneFallback(t *testing.T) {
	defer gock.Off()

	// us-central1-a never has capacity...
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Persist().
		Reply(400).
		JSON(stockoutResponse())

	// ...but us-central1-b always succeeds, with the same (preferred) machine type.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Persist().
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-b/operations/operation-name").
		Persist().
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-b/instances/agent-807jvfwj").
		Persist().
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-1"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected zone fallback with the same machine type to succeed, got error: %v", err)
	}
	if want, got := instance.Region, "us-central1-b"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
	if want, got := instance.Size, "n1-standard-1"; got != want {
		t.Errorf("Want instance Size %q, got %q", want, got)
	}
}

// TestCreateWaitsForFailedOperationBeforeNextZone covers the async stockout
// shape seen in production: the zone's insert operation reports its error
// while still RUNNING, and only reaches DONE once the instance is torn down
// and its name released. Create must wait for DONE before trying the next
// zone, so that zone's insert goes through first time - no 409 mock is
// registered, so an early retry would fail the test.
func TestCreateWaitsForFailedOperationBeforeNextZone(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-a" }`)

	stockout := map[string]interface{}{
		"errors": []map[string]interface{}{
			{
				"code":    "ZONE_RESOURCE_POOL_EXHAUSTED",
				"message": "The zone does not have enough resources available",
			},
		},
	}
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-a").
		Reply(200).
		JSON(map[string]interface{}{"status": "RUNNING", "error": stockout})
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-a").
		Reply(200).
		JSON(map[string]interface{}{"status": "DONE", "error": stockout})

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Reply(200).
		BodyString(`{ "name": "operation-b" }`)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-b/operations/operation-b").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-b/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-1"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.shuffleZones = noShuffle

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected the next zone to succeed after the stockout, got error: %v", err)
	}
	if want, got := instance.Region, "us-central1-b"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
	if pending := len(gock.Pending()); pending != 0 {
		t.Errorf("Expected every mock to be consumed, %d still pending", pending)
	}
}

func alreadyExistsResponse(zone, name string) map[string]interface{} {
	return map[string]interface{}{
		"error": map[string]interface{}{
			"code": 409,
			"errors": []map[string]interface{}{
				{"reason": "alreadyExists"},
			},
			"message": fmt.Sprintf("The resource 'projects/my-project/zones/%s/instances/%s' already exists", zone, name),
		},
	}
}

// noShuffle is a p.shuffleZones override for tests that need a
// deterministic zone visiting order instead of the real random one.
func noShuffle(zones []string) []string {
	return zones
}

// TestCreateLogsUnrecognizedInsertFailureAtError verifies that an Insert
// failure not matching any known-recoverable cause (stockout, family
// quota, rate limit, name conflict) - e.g. a permanent misconfiguration
// like a missing permission - is logged at Error, not Warn, even though
// only this one (zone, size) candidate fails.
func TestCreateLogsUnrecognizedInsertFailureAtError(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(403).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code": 403,
				"errors": []map[string]interface{}{
					{"reason": "forbidden"},
				},
				"message": "Required 'compute.images.useReadOnly' permission",
			},
		})

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	logrusLogger, hook := logrustest.NewNullLogger()
	ctx := logger.WithContext(context.TODO(), logger.Logrus(logrus.NewEntry(logrusLogger)))

	_, err = p.Create(ctx, autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err == nil {
		t.Fatalf("expected an error from the failed insert")
	}

	var found *logrus.Entry
	for _, entry := range hook.AllEntries() {
		if entry.Message == "instance insert failed" {
			found = entry
			break
		}
	}
	if found == nil {
		t.Fatalf("expected an \"instance insert failed\" log entry, got entries: %v", hook.AllEntries())
	}
	if found.Level != logrus.ErrorLevel {
		t.Errorf("expected an unrecognized insert failure to log at Error level, got %s", found.Level)
	}
	if zone, _ := found.Data["zone"].(string); zone != "us-central1-a" {
		t.Errorf("expected the zone field to be %q, got %q", "us-central1-a", zone)
	}
}

// resourceAlreadyExistsOperationResponse simulates the async shape Compute
// can also use to report a name conflict - discovered via the zone-operation
// poll, the same channel a stockout is, rather than a synchronous 409 from
// Insert itself. See isNameConflictError.
func resourceAlreadyExistsOperationResponse(message string) map[string]interface{} {
	return map[string]interface{}{
		"status": "DONE",
		"error": map[string]interface{}{
			"errors": []map[string]interface{}{
				{
					"code":    "RESOURCE_ALREADY_EXISTS",
					"message": message,
				},
			},
		},
	}
}

// TestCreateAbandonsSearchOnNameConflictWithNoPrecursor verifies that a name
// conflict with no preceding stockout or rate limit abandons the entire
// search immediately - no retry, no sweeping to another zone or machine
// type - since there's nothing to suggest it's a transient reservation
// rather than a real, permanent conflict (e.g. a different in-flight
// attempt for the same server that already succeeded).
func TestCreateAbandonsSearchOnNameConflictWithNoPrecursor(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(409).
		JSON(alreadyExistsResponse("us-central1-a", "agent-807jvfwj"))

	// Registered so that, if Create wrongly retries or sweeps instead of
	// abandoning the search, the test fails clearly via untried.Done()
	// below.
	untried := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Reply(200).
		JSON(map[string]interface{}{"name": "operation-name"})

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-1"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.shuffleZones = noShuffle

	_, err = p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err == nil {
		t.Fatalf("expected Create to abandon the search and return an error")
	}
	if untried.Done() {
		t.Errorf("expected no other zone/machine-type to be attempted")
	}
}

// TestCreateAbandonsSearchOnAsyncNameConflictWithNoPrecursor is the same as
// TestCreateAbandonsSearchOnNameConflictWithNoPrecursor but for the async
// RESOURCE_ALREADY_EXISTS operation-error shape (discovered via the
// zone-operation poll) rather than the synchronous 409.
func TestCreateAbandonsSearchOnAsyncNameConflictWithNoPrecursor(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		JSON(resourceAlreadyExistsOperationResponse("The resource 'projects/my-project/zones/us-central1-a/instances/agent-807jvfwj' already exists"))

	untried := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Reply(200).
		JSON(map[string]interface{}{"name": "operation-name"})

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-1"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.shuffleZones = noShuffle

	_, err = p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err == nil {
		t.Fatalf("expected Create to abandon the search and return an error")
	}
	if untried.Done() {
		t.Errorf("expected no other zone/machine-type to be attempted")
	}
}

func TestCreateWithMachineTypeFallback(t *testing.T) {
	defer gock.Off()

	insertInstanceMockAlt4 := *insertInstanceMock
	insertInstanceMockAlt4.MachineType = "projects/my-project/zones/us-central1-a/machineTypes/n1-standard-4"

	// Primary machine type stockouts on insert in the only configured zone.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		JSON(&insertInstanceMockAlt4).
		Reply(400).
		JSON(stockoutResponse())

	// Fallback machine type succeeds.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		JSON(insertInstanceMock).
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-4"),
		WithMachineTypeAlt([]string{"n1-standard-1"}),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}

	if want, got := instance.Size, "n1-standard-1"; got != want {
		t.Errorf("Want instance Size %q, got %q", want, got)
	}
}

// TestCreateUsesDiskTypeAltForFallbackMachineType verifies that when
// falling back to an alt machine type, the paired alt disk type (not the
// primary disk type) is used on the boot disk.
func TestCreateUsesDiskTypeAltForFallbackMachineType(t *testing.T) {
	defer gock.Off()

	insertInstanceMockAlt4 := *insertInstanceMock
	insertInstanceMockAlt4.MachineType = "projects/my-project/zones/us-central1-a/machineTypes/n1-standard-4"

	// Primary machine type stockouts on insert in the only configured zone.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		JSON(&insertInstanceMockAlt4).
		Reply(400).
		JSON(stockoutResponse())

	// Fallback machine type succeeds, using the paired alt disk type.
	insertInstanceMockDiskAlt := *insertInstanceMock
	insertInstanceMockDiskAlt.Disks = []*compute.AttachedDisk{
		{
			Type:       "PERSISTENT",
			Boot:       true,
			Mode:       "READ_WRITE",
			AutoDelete: true,
			DeviceName: "agent-807jvfwj",
			InitializeParams: &compute.AttachedDiskInitializeParams{
				SourceImage: "https://www.googleapis.com/compute/v1/projects/ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220712",
				DiskType:    "projects/my-project/zones/us-central1-a/diskTypes/pd-balanced",
				DiskSizeGb:  50,
			},
		},
	}
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		JSON(&insertInstanceMockDiskAlt).
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-4"),
		WithMachineTypeAlt([]string{"n1-standard-1"}),
		WithDiskTypeAlt([]string{"pd-balanced"}),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}
	if want, got := instance.Size, "n1-standard-1"; got != want {
		t.Errorf("Want instance Size %q, got %q", want, got)
	}
	if !gock.IsDone() {
		t.Errorf("expected the fallback insert to have used pd-balanced, not pd-standard")
	}
}

// familyQuotaOperationResponse simulates Google's async CPUS_PER_VM_FAMILY
// quota-exhaustion shape, discovered via the zone-operation poll. See
// isFamilyQuotaError.
func familyQuotaOperationResponse(message string) map[string]interface{} {
	return map[string]interface{}{
		"status": "DONE",
		"error": map[string]interface{}{
			"errors": []map[string]interface{}{
				{
					"code":    "QUOTA_EXCEEDED",
					"message": message,
				},
			},
		},
	}
}

// TestCreateSkipsRemainingZonesInRegionAfterFamilyQuota verifies that once
// a zone hits CPUS_PER_VM_FAMILY quota exhaustion, tryAllZones skips every
// remaining zone in that same region for this machine type - the quota is
// regional, not zonal, so retrying elsewhere in the region can't help -
// while still trying a zone in a different region normally.
func TestCreateSkipsRemainingZonesInRegionAfterFamilyQuota(t *testing.T) {
	defer gock.Off()

	// us-central1-a hits the family's regional CPU quota.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		JSON(familyQuotaOperationResponse("Quota 'CPUS_PER_VM_FAMILY' exceeded. Limit: 1500.0 in region us-central1."))

	// Registered so that, if Create wrongly attempts another zone in the
	// same region, the test fails clearly via untried.Done() below.
	untried := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Reply(200).
		JSON(map[string]interface{}{"name": "operation-name"})

	// A zone in a different region is unaffected and should still be tried.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-east1-b/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-east1-b/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-east1-b/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b", "us-east1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-1"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.shuffleZones = noShuffle

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected fallback to a different region to succeed, got error: %v", err)
	}
	if want, got := instance.Region, "us-east1-b"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
	if untried.Done() {
		t.Errorf("expected us-central1-b to be skipped, not attempted")
	}
}

// TestCreateSkipsZoneInStockoutCooldown verifies that a (zone, size) still
// cooling down from an earlier stockout is skipped entirely - no Insert
// call at all - while a different zone is tried normally.
func TestCreateSkipsZoneInStockoutCooldown(t *testing.T) {
	defer gock.Off()

	// Registered so that, if Create wrongly attempts the cooling-down
	// zone, the test fails clearly via untried.Done() below.
	untried := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		JSON(map[string]interface{}{"name": "operation-name"})

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-b/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-b/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-1"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.shuffleZones = noShuffle
	p.stockouts.record(zoneSize{zone: "us-central1-a", size: "n1-standard-1"}, time.Minute)

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected fallback to the uncooled zone to succeed, got error: %v", err)
	}
	if want, got := instance.Region, "us-central1-b"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
	if untried.Done() {
		t.Errorf("expected us-central1-a to be skipped, not attempted")
	}
}

// TestCreateRecordsStockoutForLaterCalls verifies that a stockout observed
// on one Create call is recorded so a later Create call against the same
// provider skips that (zone, size) too, instead of independently
// rediscovering the same stockout via a real Insert call.
func TestCreateRecordsStockoutForLaterCalls(t *testing.T) {
	defer gock.Off()

	// First call: us-central1-a stockouts, us-central1-b succeeds.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(400).
		JSON(stockoutResponse())

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Times(2).
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-b/operations/operation-name").
		Times(2).
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-b/instances/agent-807jvfwj").
		Times(2).
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	// Registered so that, if the second call wrongly re-attempts
	// us-central1-a instead of skipping it, the test fails clearly via
	// untried.Done() below.
	untried := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		JSON(map[string]interface{}{"name": "operation-name"})

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-1"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.shuffleZones = noShuffle

	if _, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"}); err != nil {
		t.Fatalf("expected the first call's fallback to succeed, got error: %v", err)
	}

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected the second call to skip the recorded stockout and succeed, got error: %v", err)
	}
	if want, got := instance.Region, "us-central1-b"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
	if untried.Done() {
		t.Errorf("expected us-central1-a to still be in cooldown on the second call, not attempted")
	}
}

// TestCreateWhenEveryZoneIsInStockoutCooldown verifies the all-skipped case
// - every configured zone already cooling down from a stockout - returns a
// distinct, descriptive error instead of making any Insert call at all.
func TestCreateWhenEveryZoneIsInStockoutCooldown(t *testing.T) {
	defer gock.Off()

	// Registered so that, if tryAllZones wrongly attempts a zone that's
	// in cooldown, the test fails clearly via untried.Done() below,
	// rather than relying on an unmocked HTTP call to fail some other way.
	untriedA := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		JSON(map[string]interface{}{"name": "operation-name"})
	untriedB := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Reply(200).
		JSON(map[string]interface{}{"name": "operation-name"})

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-1"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.shuffleZones = noShuffle
	p.stockouts.record(zoneSize{zone: "us-central1-a", size: "n1-standard-1"}, time.Minute)
	p.stockouts.record(zoneSize{zone: "us-central1-b", size: "n1-standard-1"}, time.Minute)

	_, err = p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err == nil {
		t.Fatalf("expected an error when every zone is in stockout cooldown")
	}
	if !strings.Contains(err.Error(), "cooling down") {
		t.Errorf("expected a cooldown-specific error, got %q", err.Error())
	}
	if untriedA.Done() || untriedB.Done() {
		t.Errorf("expected no Insert calls when every zone is in cooldown")
	}
}

// TestCreatePreservesRealErrorWhenSomeZonesAreInCooldown verifies the
// cooldown skip never overwrites tryAllZones's err variable, so a real,
// more informative error from an attempted zone isn't discarded in favor
// of a generic "skipping" message.
func TestCreatePreservesRealErrorWhenSomeZonesAreInCooldown(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Reply(403).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    403,
				"message": "a distinctive permission error for us-central1-b",
			},
		})

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-1"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.shuffleZones = noShuffle
	p.stockouts.record(zoneSize{zone: "us-central1-a", size: "n1-standard-1"}, time.Minute)

	_, err = p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err == nil {
		t.Fatalf("expected an error, both zones ultimately fail")
	}
	if !strings.Contains(err.Error(), "us-central1-b") {
		t.Errorf("expected the real error from us-central1-b to surface, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "cooling down") || strings.Contains(err.Error(), "skipping") {
		t.Errorf("expected the real error, not a cooldown/skipping message, got %q", err.Error())
	}
}

func TestCreateWithAllMachineTypesExhausted(t *testing.T) {
	defer gock.Off()

	stockoutReply := func() {
		gock.New("https://compute.googleapis.com").
			Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
			Reply(400).
			JSON(stockoutResponse())
	}
	stockoutReply()
	stockoutReply()

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-4"),
		WithMachineTypeAlt([]string{"n1-standard-1"}),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	_, err = p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err == nil {
		t.Fatalf("expected error when all machine types are exhausted")
	}
}

func TestCreateFallsBackOnAnyError(t *testing.T) {
	defer gock.Off()

	insertInstanceMockAlt4 := *insertInstanceMock
	insertInstanceMockAlt4.MachineType = "projects/my-project/zones/us-central1-a/machineTypes/n1-standard-4"

	// Primary machine type fails for a reason unrelated to capacity.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		JSON(&insertInstanceMockAlt4).
		Reply(403).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    403,
				"message": "insufficient permissions",
			},
		})

	// Fallback machine type succeeds.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		JSON(insertInstanceMock).
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-4"),
		WithMachineTypeAlt([]string{"n1-standard-1"}),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected fallback to be attempted despite a non-stockout error, got error: %v", err)
	}
	if want, got := instance.Size, "n1-standard-1"; got != want {
		t.Errorf("Want instance Size %q, got %q", want, got)
	}
}

func TestShuffledZones(t *testing.T) {
	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b", "us-central1-c"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)

	got := p.shuffledZones()
	want := []string{"us-central1-a", "us-central1-b", "us-central1-c"}
	if len(got) != len(want) {
		t.Fatalf("Want %d zones, got %d", len(want), len(got))
	}
	for _, zone := range want {
		if !contains(got, zone) {
			t.Errorf("Want %v to contain %q", got, zone)
		}
	}
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

// TestCreateWithNoZonesConfigured guards against a provider with no zones
// configured (e.g. constructed without New's defaults) returning a nil
// error, which would otherwise surface as an unhelpful "%!w(<nil>)".
func TestCreateWithNoZonesConfigured(t *testing.T) {
	v, err := New(WithClient(http.DefaultClient))
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.zones = nil

	_, err = p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err == nil {
		t.Fatalf("expected an error when no zones are configured")
	}
	if strings.Contains(err.Error(), "<nil>") {
		t.Errorf("expected a descriptive error, got %q", err.Error())
	}
}

// TestCreateConcurrentCallsShareRateLimitAndCooldowns exercises the shape
// production actually runs in and no other test here covers: engine's
// allocator starts one goroutine per pending server, all calling Create on
// a single shared provider, so the rate-limit window, the turn semaphore
// and the cooldown maps are all touched concurrently rather than from one
// goroutine at a time.
//
// Every mock is Persist()ed and keyed only by zone, never by call order, so
// the assertions don't depend on which goroutine reaches the backend first
// - the one thing that genuinely isn't deterministic here. The pre-recorded
// window (rather than a 429 from the backend) is what forces every call
// through waitForRateLimitTurn, so the turn is contended by all of them at
// once.
//
// Worth running under -race, which is where a regression in the shared
// state would actually surface.
func TestCreateConcurrentCallsShareRateLimitAndCooldowns(t *testing.T) {
	defer gock.Off()

	// us-central1-a never has capacity, so every goroutine that reaches it
	// records the same stockout cooldown concurrently.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Persist().
		Reply(400).
		JSON(stockoutResponse())

	// ...us-central1-b always does, so every call has a way to succeed.
	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Persist().
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-b/operations/operation-name").
		Persist().
		Reply(200).
		BodyString(`{ "status": "DONE" }`)

	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-b/instances/agent-\\d+").
		Persist().
		Reply(200).
		BodyString(`{ "networkInterfaces": [ { "accessConfigs": [ { "natIP": "1.2.3.4" } ] } ] }`)

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a", "us-central1-b"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-1"),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.shuffleZones = noShuffle

	// Starts every goroutine out rate limited, so they all queue for the
	// turn rather than racing straight to the backend.
	p.recordRateLimit(150 * time.Millisecond)

	const callers = 8
	type result struct {
		instance *autoscaler.Instance
		err      error
	}
	results := make([]result, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{
				Name: fmt.Sprintf("agent-%d", i),
			})
			results[i] = result{instance, err}
		}(i)
	}

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatalf("expected all %d concurrent Create calls to finish, they appear deadlocked on the shared rate-limit turn", callers)
	}

	for i, got := range results {
		switch {
		case got.err != nil:
			t.Errorf("caller %d: expected to fall back to us-central1-b and succeed, got error: %v", i, got.err)
		case got.instance == nil:
			t.Errorf("caller %d: returned no instance and no error", i)
		case got.instance.Region != "us-central1-b":
			t.Errorf("caller %d: Want instance Region %q, got %q", i, "us-central1-b", got.instance.Region)
		}
	}
	if p.rateLimitTurnHeld() {
		t.Errorf("expected the turn to be released once every caller finished")
	}
	if _, cooling := p.stockouts.remaining(zoneSize{"us-central1-a", "n1-standard-1"}); !cooling {
		t.Errorf("expected the stockout in us-central1-a to be recorded for later calls")
	}
}

// TestCreateRespectsSearchTimeout verifies that Create gives up once
// createSearchTimeout elapses instead of exhausting every zone/machine-type
// combination, each with its own retry budget, against a persistently
// failing backend.
func TestCreateRespectsSearchTimeout(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Persist().
		Reply(503).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    503,
				"message": "Service Unavailable",
			},
		})

	v, err := New(
		WithClient(http.DefaultClient),
		WithZones("us-central1-a"),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
		WithMachineType("n1-standard-4"),
		WithMachineTypeAlt([]string{"n1-standard-2", "n1-standard-1"}),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.createSearchTimeout = 50 * time.Millisecond

	start := time.Now()
	_, err = p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected an error once the search timeout elapses")
	}
	// generous upper bound: well under what exhausting 3 machine types'
	// full 5-attempt retry budgets against a persistent 503 would take.
	if elapsed > 2*time.Second {
		t.Errorf("expected Create to give up close to createSearchTimeout, took %s", elapsed)
	}
}

func newUnconfirmedInsertTestProvider(t *testing.T, zones ...string) *provider {
	t.Helper()
	v, err := New(
		WithClient(http.DefaultClient),
		WithZones(zones...),
		WithProject("my-project"),
		WithUserData("#cloud-init"),
	)
	if err != nil {
		t.Fatal(err)
	}
	p := v.(*provider)
	p.init.Do(func() {})
	p.maxTransientDelay = time.Millisecond
	return p
}

func rateLimitedPollMock(zone string, times int) {
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/"+zone+"/operations/operation-name").
		Times(times).
		Reply(429).
		AddHeader("Retry-After", "0").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{"code": 429, "message": "Too Many Requests"},
		})
}

// TestCreateReturnsNoInstanceOnOperationFailure verifies an
// operation that reaches DONE with an error (here a quota failure) returns no
// instance, since nothing was created.
func TestCreateReturnsNoInstanceOnOperationFailure(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		JSON(familyQuotaOperationResponse("Quota 'N2_CPUS' exceeded"))

	p := newUnconfirmedInsertTestProvider(t, "us-central1-a")

	instance, err := p.Create(context.Background(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err == nil {
		t.Fatalf("expected Create to fail")
	}
	if instance != nil {
		t.Errorf("did not expect an instance after a definitive operation failure, got %v", instance)
	}
	var unconfirmed *insertUnconfirmedError
	if errors.As(err, &unconfirmed) {
		t.Errorf("a definitive operation failure is not an unconfirmed insert, got %v", err)
	}
}

// TestCreateCandidateTimeoutStopsSearchAndReturnsInstance verifies that an
// accepted Insert whose operation never finishes is cut off by createTimeout,
// well before createSearchTimeout, and handed back for the reaper instead of
// being retried in the next zone.
func TestCreateCandidateTimeoutStopsSearchAndReturnsInstance(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Times(1).
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Persist().
		Reply(200).
		BodyString(`{ "status": "RUNNING" }`)
	nextZone := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	p := newUnconfirmedInsertTestProvider(t, "us-central1-a", "us-central1-b")
	p.shuffleZones = noShuffle
	p.createTimeout = 500 * time.Millisecond

	logrusLogger, hook := logrustest.NewNullLogger()
	ctx := logger.WithContext(context.Background(), logger.Logrus(logrus.NewEntry(logrusLogger)))

	start := time.Now()
	instance, err := p.Create(ctx, autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("expected Create to stop near createTimeout, took %s", elapsed)
	}

	var unconfirmed *insertUnconfirmedError
	if !errors.As(err, &unconfirmed) {
		t.Fatalf("expected an *insertUnconfirmedError, got %v", err)
	}
	if instance == nil || instance.ID != "agent-807jvfwj" || instance.Region != "us-central1-a" {
		t.Fatalf("expected the timed-out instance's identity to be returned, got %v", instance)
	}
	if nextZone.Done() {
		t.Errorf("expected the search to stop instead of trying the next zone")
	}

	var warned bool
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel {
			warned = true
		}
		if entry.Level <= logrus.ErrorLevel {
			t.Errorf("did not expect an error-level log for a slow response, got %q", entry.Message)
		}
	}
	if !warned {
		t.Errorf("expected a warning for a slow response, got entries: %v", hook.AllEntries())
	}
}

// runUnconfirmedInsertCase runs Create against a first zone whose mocks the
// caller has set up, with a second zone that must not be tried, and returns
// the result and the captured log.
func runUnconfirmedInsertCase(t *testing.T, createTimeout time.Duration) (*autoscaler.Instance, *gock.Response, *logrustest.Hook, error) {
	t.Helper()
	nextZone := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-b/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)

	p := newUnconfirmedInsertTestProvider(t, "us-central1-a", "us-central1-b")
	p.shuffleZones = noShuffle
	p.createTimeout = createTimeout

	logrusLogger, hook := logrustest.NewNullLogger()
	ctx := logger.WithContext(context.Background(), logger.Logrus(logrus.NewEntry(logrusLogger)))

	instance, err := p.Create(ctx, autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	return instance, nextZone, hook, err
}

func requireUnconfirmed(t *testing.T, instance *autoscaler.Instance, err error, nextZone *gock.Response) {
	t.Helper()
	var unconfirmed *insertUnconfirmedError
	if !errors.As(err, &unconfirmed) {
		t.Fatalf("expected an *insertUnconfirmedError, got %v", err)
	}
	if instance == nil || instance.ID != "agent-807jvfwj" || instance.Region != "us-central1-a" {
		t.Fatalf("expected the instance's identity to be returned, got %v", instance)
	}
	if nextZone.Done() {
		t.Errorf("expected the search to stop instead of trying the next zone")
	}
}

// TestCreateInsertTimeoutReturnsInstanceForReaper verifies that an attempt
// deadline during the Insert request itself, where Google may have accepted
// the request, hands the instance back for the reaper instead of trying the
// next zone.
func TestCreateInsertTimeoutReturnsInstanceForReaper(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		Delay(5 * time.Second).
		BodyString(`{ "name": "operation-name" }`)

	instance, nextZone, hook, err := runUnconfirmedInsertCase(t, 500*time.Millisecond)
	requireUnconfirmed(t, instance, err, nextZone)

	var warned bool
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel {
			warned = true
		}
		if entry.Level <= logrus.ErrorLevel {
			t.Errorf("did not expect an error-level log for a slow response, got %q", entry.Message)
		}
	}
	if !warned {
		t.Errorf("expected a warning for a slow response, got entries: %v", hook.AllEntries())
	}
}

// TestCreateInsertNetworkErrorReturnsInstanceForReaper verifies that an Insert
// that fails without any answer from Google is treated as possibly accepted.
func TestCreateInsertNetworkErrorReturnsInstanceForReaper(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		ReplyError(errors.New("connection reset by peer"))

	instance, nextZone, _, err := runUnconfirmedInsertCase(t, time.Minute)
	requireUnconfirmed(t, instance, err, nextZone)
}

// TestCreateInsertTransientErrorsExhaustedReturnsInstanceForReaper verifies
// that a transient status that survives every retry is treated as possibly
// accepted.
func TestCreateInsertTransientErrorsExhaustedReturnsInstanceForReaper(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Times(5).
		Reply(503).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{"code": 503, "message": "unavailable"},
		})

	instance, nextZone, _, err := runUnconfirmedInsertCase(t, time.Minute)
	requireUnconfirmed(t, instance, err, nextZone)
}

// TestCreateReadBackTimeoutReturnsInstance verifies that a deadline during
// the follow-up read logs a warning, not an error, and still hands the
// instance back for the reaper.
func TestCreateReadBackTimeoutReturnsInstance(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		BodyString(`{ "name": "operation-name" }`)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		Delay(5 * time.Second).
		BodyString(`{}`)

	instance, nextZone, hook, err := runUnconfirmedInsertCase(t, 500*time.Millisecond)
	requireUnconfirmed(t, instance, err, nextZone)

	var warned bool
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel {
			warned = true
		}
		if entry.Level <= logrus.ErrorLevel {
			t.Errorf("did not expect an error-level log for a slow response, got %q", entry.Message)
		}
	}
	if !warned {
		t.Errorf("expected a warning for a slow response, got entries: %v", hook.AllEntries())
	}
}

func TestInsertOutcomeKnown(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"rejection", &googleapi.Error{Code: 400}, true},
		{"conflict", &googleapi.Error{Code: 409}, true},
		{"rate limited", &retryAfterError{Err: &googleapi.Error{Code: 429}}, true},
		{"transient status", &googleapi.Error{Code: 503}, false},
		{"deadline", context.DeadlineExceeded, false},
		{"canceled", context.Canceled, false},
		{"network", errors.New("connection reset by peer"), false},
	}
	for _, test := range tests {
		if got := insertOutcomeKnown(test.err); got != test.want {
			t.Errorf("%s: want %v, got %v", test.name, test.want, got)
		}
	}
}

// TestNoteZoneOutcomeSkipsErrorLogOnceSearchContextExpired verifies that
// the candidates failing instantly after the search ran out of time are not
// each logged at Error, while a real failure still is.
func TestNoteZoneOutcomeSkipsErrorLogOnceSearchContextExpired(t *testing.T) {
	p := newUnconfirmedInsertTestProvider(t, "us-central1-a")
	s := &createSearch{p: p, opts: autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"}}

	logrusLogger, hook := logrustest.NewNullLogger()
	base := logger.WithContext(context.Background(), logger.Logrus(logrus.NewEntry(logrusLogger)))
	expired, cancel := context.WithTimeout(base, time.Nanosecond)
	defer cancel()
	<-expired.Done()

	s.noteZoneOutcome(expired, "us-central1-a", "n4-standard-8", "pd-standard", fmt.Errorf("insert: %w", context.DeadlineExceeded))
	if n := len(hook.AllEntries()); n != 0 {
		t.Errorf("expected no log for a deadline error after the search expired, got %v", hook.AllEntries())
	}

	// A genuine failure must still reach Error even if time has run out.
	s.noteZoneOutcome(expired, "us-central1-a", "n4-standard-8", "pd-standard", errors.New("invalid machine type"))
	// And a deadline error while the search ctx is still live is not ours to hide.
	s.noteZoneOutcome(base, "us-central1-a", "n4-standard-8", "pd-standard", context.DeadlineExceeded)
	var errorLogs int
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.ErrorLevel {
			errorLogs++
		}
	}
	if errorLogs != 2 {
		t.Errorf("expected 2 Error logs (real failure, live-ctx deadline), got %d: %v", errorLogs, hook.AllEntries())
	}
}

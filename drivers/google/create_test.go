// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"context"
	"errors"
	"net/http"
	"strings"
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

// TestCreateReturnsInstanceOnGetFailure verifies a failure reading the
// instance back after a successful insert is also an unconfirmed outcome,
// not a rejection.
func TestCreateReturnsInstanceOnGetFailure(t *testing.T) {
	defer gock.Off()

	mockAcceptedInsert()
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(404)

	p := newCreateTestProvider(t)

	instance, err := p.Create(context.Background(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})

	var unconfirmed *insertUnconfirmedError
	if !errors.As(err, &unconfirmed) {
		t.Fatalf("expected an *insertUnconfirmedError, got %v", err)
	}
	if instance == nil || instance.ID != "agent-807jvfwj" {
		t.Fatalf("expected the accepted instance to be returned, got %+v", instance)
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

func runUnconfirmedInsertCase(t *testing.T, createTimeout time.Duration) (*autoscaler.Instance, error) {
	t.Helper()
	p := newCreateTestProvider(t)
	p.createTimeout = createTimeout

	return p.Create(context.Background(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
}

func requireUnconfirmed(t *testing.T, instance *autoscaler.Instance, err error) {
	t.Helper()
	var unconfirmed *insertUnconfirmedError
	if !errors.As(err, &unconfirmed) {
		t.Fatalf("expected an *insertUnconfirmedError, got %v", err)
	}
	if instance == nil || instance.ID != "agent-807jvfwj" || instance.Region != "us-central1-a" {
		t.Fatalf("expected the instance's identity to be returned, got %v", instance)
	}
}

// TestCreateInsertTimeoutReturnsInstanceForReaper verifies that a deadline
// during the Insert request itself, where Google may have accepted the
// request, hands the instance back for the reaper instead of dropping it.
// A slow provider is expected to recover and the instance is left for the
// reaper, so it is logged as a warning, never an error.
func TestCreateInsertTimeoutReturnsInstanceForReaper(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		Delay(2 * time.Second).
		BodyString(`{ "name": "operation-name" }`)

	p := newCreateTestProvider(t)
	p.createTimeout = 100 * time.Millisecond

	logrusLogger, hook := logrustest.NewNullLogger()
	ctx := logger.WithContext(context.Background(), logger.Logrus(logrus.NewEntry(logrusLogger)))

	start := time.Now()
	instance, err := p.Create(ctx, autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	requireUnconfirmed(t, instance, err)
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
		t.Errorf("expected a warning for a slow response, got %v", hook.AllEntries())
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("expected Create to give up near createTimeout (100ms), took %s", elapsed)
	}
}

// TestCreateInsertNetworkErrorReturnsInstanceForReaper verifies that an
// Insert that fails without any answer from Google is treated as possibly
// accepted.
func TestCreateInsertNetworkErrorReturnsInstanceForReaper(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		ReplyError(errors.New("connection reset by peer"))

	instance, err := runUnconfirmedInsertCase(t, time.Minute)
	requireUnconfirmed(t, instance, err)
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

	instance, err := runUnconfirmedInsertCase(t, time.Minute)
	requireUnconfirmed(t, instance, err)
}

// TestCreateReadBackTimeoutReturnsInstance verifies that a deadline during
// the follow-up read still hands the instance back for the reaper.
func TestCreateReadBackTimeoutReturnsInstance(t *testing.T) {
	defer gock.Off()

	mockAcceptedInsert()
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Reply(200).
		BodyString(`{ "status": "DONE" }`)
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/instances/agent-807jvfwj").
		Reply(200).
		Delay(2 * time.Second).
		BodyString(`{}`)

	instance, err := runUnconfirmedInsertCase(t, 300*time.Millisecond)
	requireUnconfirmed(t, instance, err)
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
		t.Fatalf("expected the sole waiter to succeed after retrying, got error: %v", err)
	}
	if want, got := instance.Region, "us-central1-a"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
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
		t.Fatalf("expected the sole waiter to succeed after retrying, got error: %v", err)
	}
	if want, got := instance.Region, "us-central1-a"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
	}
}

// TestCreateGivesUpWaitingWhenItWouldOutlastTheSearch verifies that the
// sole waiter doesn't start a wait it can't finish: if the shared backoff
// window is longer than createTimeout has left, it abandons the
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
	p.createTimeout = time.Second

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
	p.createTimeout = 10 * time.Millisecond
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

// TestCreateReturnsInstanceOnPollRateLimitGiveUp verifies that when an
// accepted Insert's operation poll exhausts its rate-limit retries, Create
// neither re-inserts the name (the error wraps a rate limit, which is
// otherwise retried) nor drops the VM: it returns the instance's identity
// with the error, so the reaper can destroy it.
func TestCreateReturnsInstanceOnPollRateLimitGiveUp(t *testing.T) {
	defer gock.Off()

	mockAcceptedInsert()

	// A second Insert for the same name must never be made.
	reinsert := gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(409).
		JSON(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    409,
				"message": "The resource already exists",
				"errors":  []map[string]interface{}{{"reason": "alreadyExists"}},
			},
		})

	// The initial poll plus one retry, then give up - exactly this many.
	gock.New("https://compute.googleapis.com").
		Get("/compute/v1/projects/my-project/zones/us-central1-a/operations/operation-name").
		Times(2).
		Reply(429).
		AddHeader("Retry-After", "0").
		JSON(map[string]interface{}{
			"error": map[string]interface{}{"code": 429, "message": "Too Many Requests"},
		})

	p := newCreateTestProvider(t)
	p.rateLimit.maxOperationRetries = 1

	instance, err := p.Create(context.Background(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})

	var unconfirmed *insertUnconfirmedError
	if !errors.As(err, &unconfirmed) {
		t.Fatalf("expected an *insertUnconfirmedError, got %v", err)
	}
	var rl *retryAfterError
	if !errors.As(err, &rl) {
		t.Errorf("expected the error to still wrap the rate limit, got %v", err)
	}
	if instance == nil || instance.ID != "agent-807jvfwj" {
		t.Fatalf("expected the accepted instance to be returned, got %+v", instance)
	}
	if reinsert.Done() {
		t.Errorf("expected the accepted name never to be inserted a second time")
	}
}

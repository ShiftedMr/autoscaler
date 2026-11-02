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
	"github.com/drone/autoscaler/logger"
	"github.com/h2non/gock"
	"github.com/sirupsen/logrus"

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

	logrusLogger, hook := newNullLogger()
	ctx := logger.WithContext(context.Background(), logger.Logrus(logrus.NewEntry(logrusLogger)))

	instance, err := p.Create(ctx, autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})

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
	var warned bool
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && entry.Message == "slow response from provider: the instance insert did not finish in time" {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected a slow-response warning, got %v", hook.AllEntries())
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

// TestCreateTimeoutBoundsTheInsertRequestItself verifies createTimeout also
// covers asking Google to insert. Nothing was accepted, so no instance is
// returned.
func TestCreateTimeoutBoundsTheInsertRequestItself(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		Reply(200).
		Delay(2 * time.Second).
		BodyString(`{ "name": "operation-name" }`)

	p := newCreateTestProvider(t)
	p.createTimeout = 50 * time.Millisecond

	start := time.Now()
	instance, err := p.Create(context.Background(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err == nil {
		t.Fatal("expected an error when the insert outlasts createTimeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("expected Create to give up near createTimeout (50ms), took %s", elapsed)
	}
	if instance != nil {
		t.Errorf("expected no instance when the insert was never accepted, got %+v", instance)
	}
}

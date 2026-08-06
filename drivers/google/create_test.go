// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/drone/autoscaler"
	"github.com/h2non/gock"

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

// TestCreateRetriesSameZoneOnRateLimit verifies that a 429 causes Create to
// wait out the Retry-After duration and retry the same zone, rather than
// immediately moving on to a different candidate (moving on wouldn't avoid
// a project-level rate limit, and would only add more load while throttled).
func TestCreateRetriesSameZoneOnRateLimit(t *testing.T) {
	defer gock.Off()

	gock.New("https://compute.googleapis.com").
		Post("/compute/v1/projects/my-project/zones/us-central1-a/instances").
		JSON(insertInstanceMock).
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
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)
	p.init.Do(func() {})

	instance, err := p.Create(context.TODO(), autoscaler.InstanceCreateOpts{Name: "agent-807jVFwj"})
	if err != nil {
		t.Fatalf("expected the same zone to be retried after the rate limit, got error: %v", err)
	}
	if want, got := instance.Region, "us-central1-a"; got != want {
		t.Errorf("Want instance Region %q, got %q", want, got)
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

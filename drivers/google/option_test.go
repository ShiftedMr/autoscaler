// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestOptions(t *testing.T) {
	v, err := New(
		WithClient(http.DefaultClient),
		WithDiskSize(100),
		WithDiskType("local-ssd"),
		WithMachineImage("ubuntu-1604-lts"),
		WithMachineType("c4d-standard-4"),
		WithMachineTypeAlt([]string{"c4d-standard-8"}),
		WithDiskTypeAlt([]string{"pd-balanced"}),
		WithNetwork("global/defaults/foo"),
		WithPrivateIP(false),
		WithServiceAccountEmail("default"),
		WithProject("my-project"),
		WithTags("drone", "agent"),
		WithZones("us-central1-f"),
		WithScopes("scope1", "scope2"),
		WithUserDataKey("test-key"),
		WithCreateTimeout(2*time.Minute),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)

	if got, want := p.diskSize, int64(100); got != want {
		t.Errorf("Want diskSize %d, got %d", want, got)
	}
	if got, want := p.diskType, "local-ssd"; got != want {
		t.Errorf("Want diskType %s, got %s", want, got)
	}
	if got, want := p.image, "ubuntu-1604-lts"; got != want {
		t.Errorf("Want image %q, got %q", want, got)
	}
	if got, want := p.network, "global/defaults/foo"; got != want {
		t.Errorf("Want network %q, got %q", want, got)
	}
	if got, want := p.privateIP, false; got != want {
		t.Errorf("Want %v privateIP, got %v", want, got)
	}
	if got, want := p.project, "my-project"; got != want {
		t.Errorf("Want project %q, got %q", want, got)
	}
	if got, want := p.size, "c4d-standard-4"; got != want {
		t.Errorf("Want size %q, got %q", want, got)
	}
	if got, want := p.sizesAlt, []string{"c4d-standard-8"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Want sizesAlt %q, got %q", want, got)
	}
	if got, want := p.sizes(), []string{"c4d-standard-4", "c4d-standard-8"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Want combined sizes %q, got %q", want, got)
	}
	if got, want := p.diskTypesAlt, []string{"pd-balanced"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Want diskTypesAlt %q, got %q", want, got)
	}
	if got, want := p.diskTypes(), []string{"local-ssd", "pd-balanced"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Want combined diskTypes %q, got %q", want, got)
	}
	if got, want := len(p.tags), 2; got != want {
		t.Errorf("Want %d tags, got %d", want, got)
	}
	if got, want := p.zones, []string{"us-central1-f"}; !reflect.DeepEqual(want, got) {
		t.Errorf("Want zone %q, got %q", want, got)
	}
	if got, want := len(p.scopes), 2; got != want {
		t.Errorf("Want %d scopes, got %d", want, got)
	}
	if got, want := p.serviceAccountEmail, "default"; got != want {
		t.Errorf("Want service account name %q, got %q", want, got)
	}
	if got, want := p.userdataKey, "test-key"; got != want {
		t.Errorf("Want userdata key %q, got %q", want, got)
	}
	if got, want := p.createTimeout, 2*time.Minute; got != want {
		t.Errorf("Want createTimeout %s, got %s", want, got)
	}
}

// TestMachineAndDiskTypeAltTrimWhitespace guards against the whitespace
// envconfig leaves behind when splitting a comma-separated env var: e.g.
// DRONE_GOOGLE_MACHINE_TYPE_ALT="n1-standard-2, n1-standard-4" produces
// []string{"n1-standard-2", " n1-standard-4"} (leading space on the second
// entry), which GCE would reject as an invalid resource name.
func TestMachineAndDiskTypeAltTrimWhitespace(t *testing.T) {
	v, err := New(
		WithClient(http.DefaultClient),
		WithMachineType("n1-standard-4"),
		WithMachineTypeAlt([]string{" n1-standard-2", "n1-standard-1 ", "  n1-standard-8  "}),
		WithDiskType("pd-ssd"),
		WithDiskTypeAlt([]string{"pd-balanced ", " pd-standard", "  pd-extreme  "}),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)

	if got, want := p.sizesAlt, []string{"n1-standard-2", "n1-standard-1", "n1-standard-8"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Want trimmed sizesAlt %q, got %q", want, got)
	}
	if got, want := p.diskTypesAlt, []string{"pd-balanced", "pd-standard", "pd-extreme"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Want trimmed diskTypesAlt %q, got %q", want, got)
	}
}

func TestMachineAndDiskTypeAltNilWhenUnset(t *testing.T) {
	v, err := New(
		WithClient(http.DefaultClient),
		WithMachineTypeAlt(nil),
		WithDiskTypeAlt(nil),
	)
	if err != nil {
		t.Error(err)
		return
	}
	p := v.(*provider)

	if p.sizesAlt != nil {
		t.Errorf("Want nil sizesAlt when unset, got %q", p.sizesAlt)
	}
	if p.diskTypesAlt != nil {
		t.Errorf("Want nil diskTypesAlt when unset, got %q", p.diskTypesAlt)
	}
}

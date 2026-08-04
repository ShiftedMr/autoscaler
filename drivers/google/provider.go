// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"text/template"
	"time"

	"github.com/drone/autoscaler"
	"github.com/drone/autoscaler/drivers/internal/userdata"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/time/rate"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
)

var (
	defaultTags = []string{
		"allow-docker",
	}

	defaultScopes = []string{
		"https://www.googleapis.com/auth/devstorage.read_only",
		"https://www.googleapis.com/auth/logging.write",
		"https://www.googleapis.com/auth/monitoring.write",
		"https://www.googleapis.com/auth/trace.append",
	}
)

// provider implements a Google Cloud Platform provider.
type provider struct {
	init sync.Once

	diskSize            int64
	diskType            string
	image               string
	labels              map[string]string
	network             string
	subnetwork          string
	stackType           string
	project             string
	privateIP           bool
	scopes              []string
	serviceAccountEmail string
	size                string
	sizesAlt            []string
	tags                []string
	zones               []string
	userdata            *template.Template
	userdataKey         string

	rateLimiter *rate.Limiter

	sizeCooldown time.Duration
	sizeMu       sync.Mutex
	sizeFailures map[sizeZoneKey]time.Time

	service *compute.Service
}

// New returns a new Google Cloud Platform provider.
func New(opts ...Option) (autoscaler.Provider, error) {
	p := new(provider)
	for _, opt := range opts {
		opt(p)
	}
	if p.diskSize == 0 {
		p.diskSize = 50
	}
	if p.diskType == "" {
		p.diskType = "pd-standard"
	}
	if len(p.zones) == 0 {
		p.zones = []string{"us-central1-a"}
	}
	if p.size == "" {
		p.size = "n1-standard-1"
	}
	if p.sizeCooldown == 0 {
		p.sizeCooldown = 10 * time.Minute
	}
	if p.sizeFailures == nil {
		p.sizeFailures = map[sizeZoneKey]time.Time{}
	}
	if p.image == "" {
		p.image = "ubuntu-os-cloud/global/images/ubuntu-2004-focal-v20220712"
	}
	if p.network == "" {
		p.network = "global/networks/default"
	}
	if p.stackType == "" {
		p.stackType = "IPV4_ONLY"
	}
	if p.userdata == nil {
		p.userdata = userdata.T
	}
	if p.userdataKey == "" {
		p.userdataKey = "user-data"
	}
	if len(p.tags) == 0 {
		p.tags = defaultTags
	}
	if len(p.scopes) == 0 {
		p.scopes = defaultScopes
	}
	if p.serviceAccountEmail == "" {
		p.serviceAccountEmail = "default"
	}

	if p.rateLimiter == nil {
		// If unspecified, set to the max read rate limit for the API 25/s
		// Source: https://cloud.google.com/compute/docs/api-rate-limits
		p.rateLimiter = rate.NewLimiter(rate.Every(time.Second/25), 1)
	}

	if p.service == nil {
		client, err := google.DefaultClient(oauth2.NoContext, compute.ComputeScope)
		if err != nil {
			return nil, err
		}
		p.service, err = compute.New(client)
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}

// operationError preserves Google's structured Code/Message operation-error
// fields instead of collapsing them to a plain string.
type operationError struct {
	Code    string
	Message string
}

func (e *operationError) Error() string {
	return e.Message
}

func (p *provider) sizes() []string {
	return append([]string{p.size}, p.sizesAlt...)
}

// sizeZoneKey scopes cooldown state to a specific zone, since a stockout is
// a property of a (zone, machine type) pair, not of the machine type alone.
type sizeZoneKey struct {
	zone string
	size string
}

// availableZones returns p.zones in random order with any zone currently
// cooling down for size filtered out. If every zone is cooling down, the
// cooldown is ignored and the full list is returned instead.
func (p *provider) availableZones(size string) []string {
	zones := make([]string, len(p.zones))
	copy(zones, p.zones)
	rand.Shuffle(len(zones), func(i, j int) {
		zones[i], zones[j] = zones[j], zones[i]
	})

	p.sizeMu.Lock()
	defer p.sizeMu.Unlock()

	now := time.Now()
	available := make([]string, 0, len(zones))
	for _, zone := range zones {
		key := sizeZoneKey{zone: zone, size: size}
		if failedAt, ok := p.sizeFailures[key]; ok && now.Sub(failedAt) < p.sizeCooldown {
			continue
		}
		available = append(available, zone)
	}
	if len(available) == 0 {
		return zones
	}
	return available
}

func (p *provider) markSizeFailed(zone, size string) {
	p.sizeMu.Lock()
	defer p.sizeMu.Unlock()
	p.sizeFailures[sizeZoneKey{zone: zone, size: size}] = time.Now()
}

func (p *provider) waitZoneOperation(ctx context.Context, name string, zone string) error {
	var op *compute.Operation
	for {
		err := doWithRetry(ctx, fmt.Sprintf("zone operations.get %s", name), func() error {
			if err := p.rateLimiter.Wait(ctx); err != nil {
				return err
			}
			var err error
			op, err = p.service.ZoneOperations.Get(p.project, zone, name).Context(ctx).Do()
			if err != nil {
				if gerr, ok := err.(*googleapi.Error); ok &&
					gerr.Code == http.StatusNotFound {
					return autoscaler.ErrInstanceNotFound
				}
				return err
			}
			if op == nil {
				return errors.New("zone operation get returned nil response")
			}
			return nil
		})
		if err != nil {
			if rl, ok := err.(*retryAfterError); ok {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(rl.retryAfter):
				}
				continue
			}
			return err
		}

		if op.Error != nil && len(op.Error.Errors) > 0 {
			return &operationError{
				Code:    op.Error.Errors[0].Code,
				Message: op.Error.Errors[0].Message,
			}
		}
		if op.Status == "DONE" {
			return nil
		}

		// Keep polling cadence at ~1s while still honoring cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (p *provider) waitGlobalOperation(ctx context.Context, name string) error {
	var op *compute.Operation
	for {
		err := doWithRetry(ctx, fmt.Sprintf("global operations.get %s", name), func() error {
			if err := p.rateLimiter.Wait(ctx); err != nil {
				return err
			}
			var err error
			op, err = p.service.GlobalOperations.Get(p.project, name).Context(ctx).Do()
			if err != nil {
				return err
			}
			if op == nil {
				return errors.New("global operation get returned nil response")
			}
			return nil
		})
		if err != nil {
			if rl, ok := err.(*retryAfterError); ok {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(rl.retryAfter):
				}
				continue
			}
			return err
		}

		if op.Error != nil && len(op.Error.Errors) > 0 {
			return &operationError{
				Code:    op.Error.Errors[0].Code,
				Message: op.Error.Errors[0].Message,
			}
		}
		if op.Status == "DONE" {
			return nil
		}

		// Keep polling cadence at ~1s while still honoring cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

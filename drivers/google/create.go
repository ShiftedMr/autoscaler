// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/drone/autoscaler"
	"github.com/drone/autoscaler/logger"
	"github.com/google/uuid"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
)

// Create provisions an instance, trying every configured machine type across
// every configured zone before giving up. createSearchTimeout bounds the
// total time spent doing so: an async stockout is only discovered after
// actually polling the zone operation for real wall-clock time, and that
// cost multiplies by every (zone, size) combination, so without a cap a
// broad outage would slowly work through the full list before the caller's
// own (much longer) context deadline finally cuts it off, needlessly
// delaying failure detection.
func (p *provider) Create(ctx context.Context, opts autoscaler.InstanceCreateOpts) (*autoscaler.Instance, error) {
	ctx, cancel := context.WithTimeout(ctx, p.createSearchTimeout)
	defer cancel()

	err := errors.New("no machine types or zones configured")

	// tryAllZones attempts size in every configured zone (random order),
	// continuing past non-stockout errors too. A rate-limit response waits
	// out the server-requested backoff and retries the same zone, since
	// moving to a different zone won't avoid a project-level rate limit.
	tryAllZones := func(size string) (*autoscaler.Instance, error) {
		var instance *autoscaler.Instance
		// Must stay non-nil: if p.zones is ever empty the loop below never
		// runs, and this is what gets wrapped into the error Create
		// ultimately returns. See TestCreateWithNoZonesConfigured.
		err := fmt.Errorf("no zones configured for machine type %q", size)
		for _, zone := range p.shuffledZones() {
			for {
				instance, err = p.createInZone(ctx, opts, zone, size)
				if instance != nil {
					return instance, err
				}
				rl, ok := err.(*retryAfterError)
				if !ok {
					break
				}
				logger.FromContext(ctx).
					WithField("zone", zone).
					WithField("size", size).
					WithField("retryAfter", rl.retryAfter).
					Infoln("rate limited, waiting before retrying the same zone")
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(rl.retryAfter):
				}
			}
			if isStockoutError(err) {
				logger.FromContext(ctx).
					WithField("zone", zone).
					WithField("size", size).
					WithError(err).
					Infoln("machine type unavailable in zone, trying next zone")
			}
		}
		return nil, err
	}

	var instance *autoscaler.Instance
	for _, size := range p.sizes() {
		instance, err = tryAllZones(size)
		if instance != nil {
			return instance, err
		}
	}

	return nil, fmt.Errorf("failed to create instance, all machine types and zones exhausted: %w", err)
}

// createInZone provisions a single instance of size in zone, without any
// zone or machine-type fallback of its own; see Create for that.
func (p *provider) createInZone(ctx context.Context, opts autoscaler.InstanceCreateOpts, zone string, size string) (*autoscaler.Instance, error) {
	p.init.Do(func() {
		p.setup(ctx)
	})

	buf := new(bytes.Buffer)
	err := p.userdata.Execute(buf, &opts)
	if err != nil {
		return nil, err
	}

	name := strings.ToLower(opts.Name)

	logger := logger.FromContext(ctx).
		WithField("zone", zone).
		WithField("image", p.image).
		WithField("size", size).
		WithField("name", opts.Name)

	logger.Debugln("instance insert")

	networkConfig := []*compute.AccessConfig{}
	if !p.privateIP {
		networkConfig = []*compute.AccessConfig{
			{
				Name: "External NAT",
				Type: "ONE_TO_ONE_NAT",
			},
		}
	}

	in := &compute.Instance{
		Name:           name,
		Zone:           fmt.Sprintf("projects/%s/zones/%s", p.project, zone),
		MinCpuPlatform: "Automatic",
		MachineType:    fmt.Sprintf("projects/%s/zones/%s/machineTypes/%s", p.project, zone, size),
		Metadata: &compute.Metadata{
			Items: []*compute.MetadataItems{
				{
					Key:   p.userdataKey,
					Value: googleapi.String(buf.String()),
				},
			},
		},
		Tags: &compute.Tags{
			Items: p.tags,
		},
		Disks: []*compute.AttachedDisk{
			{
				Type:       "PERSISTENT",
				Boot:       true,
				Mode:       "READ_WRITE",
				AutoDelete: true,
				DeviceName: name,
				InitializeParams: &compute.AttachedDiskInitializeParams{
					SourceImage: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s", p.image),
					DiskType:    fmt.Sprintf("projects/%s/zones/%s/diskTypes/%s", p.project, zone, p.diskType),
					DiskSizeGb:  p.diskSize,
				},
			},
		},
		CanIpForward: false,
		NetworkInterfaces: []*compute.NetworkInterface{
			{
				Network:       p.network,
				Subnetwork:    p.subnetwork,
				StackType:     p.stackType,
				AccessConfigs: networkConfig,
			},
		},
		Labels: p.labels,
		Scheduling: &compute.Scheduling{
			Preemptible:       false,
			OnHostMaintenance: "MIGRATE",
			AutomaticRestart:  googleapi.Bool(true),
		},
		DeletionProtection: false,
		ServiceAccounts: []*compute.ServiceAccount{
			{
				Scopes: p.scopes,
				Email:  p.serviceAccountEmail,
			},
		},
	}

	// Cannot add this in the same way as v4 access configs since the instance creation
	// fails if any v6 access configs are specified for an instance with IPV4_ONLY stack type
	if p.stackType == "IPV4_IPV6" {
		in.NetworkInterfaces[0].Ipv6AccessConfigs = []*compute.AccessConfig{
			{
				Name:        "external-ipv6",
				Type:        "DIRECT_IPV6",
				NetworkTier: "PREMIUM",
			},
		}
	}

	// This UUID is used to make sure retried inserts are treated as the same call
	// in case an insert returns a transient error but is still actioned on google's
	// api side.
	requestID := uuid.New().String()

	var op *compute.Operation
	err = doWithRetry(ctx, "instances.insert", func() error {
		var err error
		op, err = p.service.Instances.Insert(p.project, zone, in).RequestId(requestID).Context(ctx).Do()
		return err
	})
	if err != nil {
		logger.WithError(err).
			Errorln("instance insert failed")
		return nil, err
	}

	logger.Debugln("pending instance insert operation")
	// TODO: may be worth moving these pollings to a separate loop in Allocate
	// that way polling is decoupled from the initial creation loop and can be more
	// robust between insert calls / and be safe during autoscaler restarts.
	err = p.waitZoneOperation(ctx, op.Name, zone)
	if err != nil {
		entry := logger.WithError(err)
		// operationError.Error() only renders Message; surface Code (e.g.
		// "QUOTA_EXCEEDED") as a field too so it isn't lost from the log.
		var opErr *operationError
		if errors.As(err, &opErr) && opErr.Code != "" {
			entry = entry.WithField("code", opErr.Code)
		}
		entry.Errorln("instance insert operation failed")
		return nil, err
	}

	logger.Debugln("instance insert operation complete")

	var resp *compute.Instance
	err = doWithRetry(ctx, "instances.get", func() error {
		var err error
		resp, err = p.service.Instances.Get(p.project, zone, name).Context(ctx).Do()
		return err
	})
	if err != nil {
		logger.WithError(err).
			Errorln("cannot get instance details")
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("instances.Get returned no details")
	}

	address := resp.NetworkInterfaces[0].NetworkIP

	if !p.privateIP {
		address = resp.NetworkInterfaces[0].AccessConfigs[0].NatIP
	}

	instance := &autoscaler.Instance{
		Provider:            autoscaler.ProviderGoogle,
		ID:                  name,
		Name:                opts.Name,
		Image:               p.image,
		Region:              zone,
		Size:                size,
		Address:             address,
		ServiceAccountEmail: p.serviceAccountEmail,
		Scopes:              p.scopes,
	}

	logger.
		WithField("name", instance.Name).
		WithField("ip", instance.Address).
		Debugln("instance inserted")

	return instance, nil
}

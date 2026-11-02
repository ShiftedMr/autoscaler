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

// abandonSearchError signals a failure that applies to every remaining
// candidate, not just this zone or machine type, so Create stops entirely
// rather than trying the next one. Used for a name conflict: every candidate
// in a call shares the same instance name, so all are equally blocked.
type abandonSearchError struct {
	Err error
}

// Error implements the error interface.
func (e *abandonSearchError) Error() string { return e.Err.Error() }

// Unwrap lets errors.Is/errors.As see through to the underlying error.
func (e *abandonSearchError) Unwrap() error { return e.Err }

// insertUnconfirmedError signals that Google may have accepted an Insert (so
// the name may be reserved and the VM may exist) but its outcome couldn't be
// confirmed: the Insert got no definitive answer, the operation poll gave up
// or ctx ended, or the follow-up Get failed. It is not an Insert rejection
// and must never be retried as one - re-inserting the same name just 409s.
//
// It is always returned with a partial *autoscaler.Instance (ID and Region).
// That lets the engine record the VM on the errored server, so the reaper
// destroys it instead of only deleting the DB row (empty server.ID) and
// orphaning a running VM.
type insertUnconfirmedError struct {
	Err error
}

// Error implements the error interface.
func (e *insertUnconfirmedError) Error() string {
	return fmt.Sprintf("instance insert may have been accepted but its outcome could not be confirmed: %s", e.Err)
}

// Unwrap lets errors.Is/errors.As see through to the underlying error.
func (e *insertUnconfirmedError) Unwrap() error { return e.Err }

// rateLimitBackoff reports the remaining duration of the shared rate-limit
// backoff window recorded by recordRateLimit, if one is still in effect.
func (p *provider) rateLimitBackoff() (time.Duration, bool) {
	p.rateLimit.mu.Lock()
	defer p.rateLimit.mu.Unlock()
	remaining := time.Until(p.rateLimit.until)
	if remaining <= 0 {
		return 0, false
	}
	return remaining, true
}

// recordRateLimit starts (or extends) the shared rate-limit backoff window,
// so every other Create call - concurrent or subsequent - sees the same
// signal instead of independently rediscovering an already-exhausted
// quota. It never shrinks an existing window.
func (p *provider) recordRateLimit(after time.Duration) {
	p.rateLimit.mu.Lock()
	defer p.rateLimit.mu.Unlock()
	until := time.Now().Add(after)
	if until.After(p.rateLimit.until) {
		p.rateLimit.until = until
	}
}

// attemptInZone makes one createInZone attempt, then - in this order -
// records any rate limit hit and releases the turn this attempt was holding,
// if any. Recording before releasing keeps the shared window already
// extended by the time another goroutine can claim the turn next; recording
// afterward would leave a window where that goroutine claims the turn, sees
// no active backoff yet, and slips in an unpaced attempt. waited reports
// whether this attempt held the turn via waitForRateLimitTurn; its release is
// deferred so it still runs if createInZone panics, rather than leaving the
// turn stuck claimed and wedging every other Create call on this provider.
func (p *provider) attemptInZone(ctx context.Context, opts autoscaler.InstanceCreateOpts, zone, size, diskType string, waited bool) (*autoscaler.Instance, error) {
	if waited {
		defer p.rateLimit.turn.Release(1)
	}
	// Bounds this attempt only, not the wait for a turn.
	ctx, cancel := context.WithTimeout(ctx, p.createTimeout)
	defer cancel()
	instance, err := p.createInZone(ctx, opts, zone, size, diskType)
	var rl *retryAfterError
	if errors.As(err, &rl) {
		p.recordRateLimit(rl.retryAfter)
	}
	return instance, err
}

// waitUntilFits reports an error if waiting out the given duration would
// exceed ctx's deadline, rather than starting a wait that ctx would cut off
// before it ends - which would just surface as an opaque context deadline
// exceeded instead of a clear rate-limited signal.
func waitUntilFits(ctx context.Context, wait time.Duration) error {
	if deadline, ok := ctx.Deadline(); ok && time.Now().Add(wait).After(deadline) {
		return fmt.Errorf("rate limited: waiting %s would exceed this call's remaining time (%s) - giving up rather than waiting into a deadline", wait, time.Until(deadline))
	}
	return nil
}

// waitForRateLimitTurn blocks until this call may make the next
// rate-limited attempt. It waits out the shared backoff window (rechecking
// if another call's attempt extended it while waiting), then claims the
// turn, queueing in FIFO order behind any other call already waiting for
// one. Returns an error, without attempting, if ctx is cancelled or waiting
// further would exceed its deadline.
func (p *provider) waitForRateLimitTurn(ctx context.Context) error {
	for {
		if remaining, limited := p.rateLimitBackoff(); limited {
			if err := waitUntilFits(ctx, remaining); err != nil {
				return err
			}
			if err := sleepOrDone(ctx, remaining); err != nil {
				return err
			}
			continue // window may have been extended while waiting; recheck
		}
		// Acquiring claims the turn to attempt next: only one call at a
		// time should be attempting one, so the rest aren't independently
		// adding load to an exhausted quota. Whoever claims it must release
		// it once its attempt resolves (see attemptInZone).
		if err := p.rateLimit.turn.Acquire(ctx, 1); err != nil {
			return err
		}
		// Whoever we took the turn from may have just extended the window
		// (recordRateLimit runs before their release - see attemptInZone)
		// in the instant between our check above and claiming it here. If
		// so, give it back and wait the window out properly.
		if _, limited := p.rateLimitBackoff(); limited {
			p.rateLimit.turn.Release(1)
			continue
		}
		return nil
	}
}

// createSearch carries the state one Create call accumulates.
type createSearch struct {
	p    *provider
	opts autoscaler.InstanceCreateOpts

	// rateLimitHitsWithoutWaiting counts consecutive rate-limit hits this
	// Create call has taken without an intervening wait via
	// waitForRateLimitTurn. A properly paced hit resets rather than
	// increments this (see rateLimitState.maxWaitRetries): waiting out a
	// real rate limit isn't hammering the API and shouldn't give up early.
	rateLimitHitsWithoutWaiting int
}

// Create provisions an instance, trying every configured machine type
// across every configured zone before giving up. createSearchTimeout
// bounds the total search time, since an async stockout is only detected
// by polling, and that cost multiplies across every (zone, size) tried.
//
// A rate-limit response is treated as project/region-wide, not specific to
// one (zone, size): every candidate across every concurrent Create call
// waits its turn via waitForRateLimitTurn instead of retrying
// independently, so only one attempt is ever in flight against an
// already-exhausted quota.
func (p *provider) Create(ctx context.Context, opts autoscaler.InstanceCreateOpts) (*autoscaler.Instance, error) {
	ctx, cancel := context.WithTimeout(ctx, p.createSearchTimeout)
	defer cancel()

	s := &createSearch{p: p, opts: opts}

	err := errors.New("no machine types or zones configured")
	sizes := p.sizes()
	diskTypes := p.diskTypes()

	var instance *autoscaler.Instance
	for i, size := range sizes {
		instance, err = s.tryAllZones(ctx, size, diskTypes[i])
		if instance != nil {
			return instance, err
		}
		var abandon *abandonSearchError
		if errors.As(err, &abandon) {
			// Stop trying other machine types too; err already explains
			// why (see abandonSearchError).
			return nil, err
		}
		// No special case needed for a rate-limit give-up: the next size's
		// first attempt hits the same check, and rateLimitHitsWithoutWaiting
		// is shared across sizes, so it gives up the same way.
	}

	return nil, fmt.Errorf("failed to create instance, no available machine types or zones: %w", err)
}

// tryAllZones attempts size (using diskType for its boot disk) in every
// configured zone (random order), continuing past each error - see tryZone
// for what happens on any one zone, and noteZoneOutcome for what a zone's
// final outcome means for the rest of this search.
func (s *createSearch) tryAllZones(ctx context.Context, size, diskType string) (*autoscaler.Instance, error) {
	var instance *autoscaler.Instance
	var stop bool
	// Must stay non-nil: if p.zones is ever empty the loop below never
	// runs, and this is what gets wrapped into the error Create
	// ultimately returns. See TestCreateWithNoZonesConfigured.
	err := fmt.Errorf("no zones configured for machine type %q", size)
	var attempted, skipped int
	for _, zone := range s.p.shuffledZones() {
		if remaining, cooling := s.p.familyCooldowns.remaining(familyKey(zone, size)); cooling {
			// Never touch err here: it may already hold a more
			// informative error from an earlier zone (shuffle order makes
			// this nondeterministic), and this candidate isn't attempted
			// at all.
			skipped++
			logger.FromContext(ctx).
				WithField("zone", zone).
				WithField("size", size).
				WithField("remaining", remaining).
				Infoln("skipping zone whose machine family recently hit a CPU quota in this region")
			continue
		}
		if remaining, cooling := s.p.stockouts.remaining(zoneSize{zone, size}); cooling {
			// Same reasoning as the family-quota skip above.
			skipped++
			logger.FromContext(ctx).
				WithField("zone", zone).
				WithField("size", size).
				WithField("remaining", remaining).
				Infoln("skipping zone that recently stocked out for this machine type")
			continue
		}
		attempted++
		instance, err, stop = s.tryZone(ctx, zone, size, diskType)
		if instance != nil {
			return instance, err
		}
		if stop {
			return nil, err
		}
		s.noteZoneOutcome(ctx, zone, size, diskType, err)
	}
	if attempted == 0 && skipped > 0 {
		return nil, fmt.Errorf("every zone for machine type %q is cooling down from a recent stockout or quota exhaustion", size)
	}
	return nil, err
}

// tryZone attempts size (using diskType for its boot disk) in zone, waiting
// out any rate limit first and retrying after one (see waitTurnIfLimited,
// noteRateLimitHit). stopZones reports whether tryAllZones should give up on
// this size entirely; Create's *abandonSearchError check further
// distinguishes abandoning everything from trying the next machine type.
func (s *createSearch) tryZone(ctx context.Context, zone, size, diskType string) (instance *autoscaler.Instance, err error, stopZones bool) {
	for {
		waited, err := s.waitTurnIfLimited(ctx, zone, size)
		if err != nil {
			return nil, err, true
		}

		instance, err = s.p.attemptInZone(ctx, s.opts, zone, size, diskType, waited)
		// Any instance, even with an error, means Google accepted the
		// insert (see insertUnconfirmedError): stop the search, and never
		// retry it, even though the error may wrap a rate limit.
		if instance != nil {
			return instance, err, false
		}

		var rl *retryAfterError
		if errors.As(err, &rl) {
			if giveUpErr := s.noteRateLimitHit(ctx, zone, size, rl, waited); giveUpErr != nil {
				return nil, giveUpErr, true
			}
			continue
		}

		if isNameConflictError(err) {
			logger.FromContext(ctx).
				WithField("zone", zone).
				WithField("size", size).
				WithField("name", s.opts.Name).
				WithError(err).
				Errorln("instance name already in use, abandoning the rest of this search")
			return nil, &abandonSearchError{Err: err}, true
		}
		return nil, err, false
	}
}

// waitTurnIfLimited waits out the shared rate-limit turn if one is
// currently in effect (see waitForRateLimitTurn), reporting whether this
// attempt held it. Returns an error, without attempting, if ctx is
// cancelled or waiting would exceed its deadline.
func (s *createSearch) waitTurnIfLimited(ctx context.Context, zone, size string) (waited bool, err error) {
	if _, limited := s.p.rateLimitBackoff(); !limited {
		return false, nil
	}
	logger.FromContext(ctx).
		WithField("zone", zone).
		WithField("size", size).
		Infoln("rate limited, waiting for a turn to attempt")
	if err := s.p.waitForRateLimitTurn(ctx); err != nil {
		logger.FromContext(ctx).
			WithField("zone", zone).
			WithField("size", size).
			WithError(err).
			Infoln("rate limited, giving up while waiting for a turn")
		return false, err
	}
	return true, nil
}

// noteRateLimitHit updates this Create call's give-up accounting for a
// rate-limit hit (see rateLimitState.maxWaitRetries), returning a non-nil
// error only once the consecutive-unpaced-hit cap is exceeded - meaning the
// caller should give up.
func (s *createSearch) noteRateLimitHit(ctx context.Context, zone, size string, rl *retryAfterError, waited bool) error {
	if waited {
		// A paced hit isn't hammering the API - reset rather than count it.
		s.rateLimitHitsWithoutWaiting = 0
		return nil
	}
	s.rateLimitHitsWithoutWaiting++
	if s.rateLimitHitsWithoutWaiting <= s.p.rateLimit.maxWaitRetries {
		return nil
	}
	logger.FromContext(ctx).
		WithField("zone", zone).
		WithField("size", size).
		WithField("hits", s.rateLimitHitsWithoutWaiting).
		Infoln("rate limited, giving up - hit the limit too many times in a row without a chance to wait")
	return fmt.Errorf("rate limited: hit the limit %d times in a row without waiting - giving up: %w", s.rateLimitHitsWithoutWaiting, rl)
}

// noteZoneOutcome logs and records a zone's final outcome, and is the sole
// place that decides what level that outcome warrants - createInZone
// deliberately never logs an insert failure itself, so each outcome is logged
// exactly once here, with the full picture of what this candidate's error
// actually is.
func (s *createSearch) noteZoneOutcome(ctx context.Context, zone, size, diskType string, err error) {
	switch {
	case isFamilyQuotaError(err):
		s.p.familyCooldowns.record(familyKey(zone, size), s.p.familyQuotaCooldown)
		logger.FromContext(ctx).
			WithField("zone", zone).
			WithField("size", size).
			WithField("cooldown", s.p.familyQuotaCooldown).
			WithError(err).
			Infoln("machine family vCPU quota exhausted in this region, marking the family unavailable region-wide for the cooldown period")
	case isStockoutError(err):
		s.p.stockouts.record(zoneSize{zone, size}, s.p.stockoutCooldown)
		logger.FromContext(ctx).
			WithField("zone", zone).
			WithField("size", size).
			WithField("cooldown", s.p.stockoutCooldown).
			WithError(err).
			Infoln("machine type unavailable in zone, trying next zone")
	case ctx.Err() != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)):
		// The search itself is out of time, so each remaining candidate
		// fails instantly with the same context error; logging every one
		// is noise.
	default:
		// Unrecognized - most likely a permanent misconfiguration (bad
		// image permission, invalid machine type, etc.) rather than
		// something expected to clear on retry, so it needs to reach
		// Error-level alerting even if a later candidate happens to
		// succeed and Create as a whole doesn't fail.
		var opErr *operationError
		entry := logger.FromContext(ctx).
			WithField("zone", zone).
			WithField("size", size).
			WithField("image", s.p.image).
			WithField("diskType", diskType).
			WithField("name", s.opts.Name).
			WithError(err)
		if errors.As(err, &opErr) && opErr.Code != "" {
			entry = entry.WithField("code", opErr.Code)
		}
		entry.Errorln("instance insert failed")
	}
}

// createInZone provisions a single instance of size (with a boot disk of
// diskType) in zone, without any zone or machine-type fallback of its own;
// see Create for that.
func (p *provider) createInZone(ctx context.Context, opts autoscaler.InstanceCreateOpts, zone, size, diskType string) (*autoscaler.Instance, error) {
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
		WithField("diskType", diskType).
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
					DiskType:    fmt.Sprintf("projects/%s/zones/%s/diskTypes/%s", p.project, zone, diskType),
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

	// An expired ctx means no request was sent, so there is nothing to
	// clean up.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var op *compute.Operation
	err = doWithRetry(ctx, "instances.insert", p.maxTransientDelay, func() error {
		var err error
		op, err = p.service.Instances.Insert(p.project, zone, in).RequestId(requestID).Context(ctx).Do()
		return err
	})
	if err != nil {
		if !insertOutcomeKnown(err) {
			if errors.Is(err, context.DeadlineExceeded) {
				logger.WithError(err).
					Warnln("slow response from provider: the instance insert request did not complete in time, leaving the instance for the reaper")
			}
			return p.unconfirmedInsert(opts, name, zone, size, err)
		}
		return nil, err
	}

	logger.Debugln("pending instance insert operation")
	// TODO: may be worth moving these pollings to a separate loop in Allocate
	// that way polling is decoupled from the initial creation loop and can be more
	// robust between insert calls / and be safe during autoscaler restarts.
	err = p.waitZoneOperation(ctx, op.Name, zone)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			logger.WithError(err).
				Warnln("slow response from provider: the instance insert did not finish in time, leaving the instance for the reaper")
		}
		return p.unconfirmedInsert(opts, name, zone, size, err)
	}

	logger.Debugln("instance insert operation complete")

	var resp *compute.Instance
	err = doWithRetry(ctx, "instances.get", p.maxTransientDelay, func() error {
		var err error
		resp, err = p.service.Instances.Get(p.project, zone, name).Context(ctx).Do()
		return err
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			logger.WithError(err).
				Warnln("slow response from provider: the instance details were not available in time, leaving the instance for the reaper")
		} else {
			logger.WithError(err).
				Errorln("cannot get instance details")
		}
		return p.unconfirmedInsert(opts, name, zone, size, err)
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

// insertOutcomeKnown reports whether err from an Insert is a definitive answer
// from Google (a rejection such as a stockout, quota, rate limit or conflict),
// so nothing was created. Anything else - a context error, a network error, or
// a transient status that survived every retry - leaves the outcome unknown:
// the request may have been accepted.
func insertOutcomeKnown(err error) bool {
	var gerr *googleapi.Error
	return errors.As(err, &gerr) && !isTransientError(gerr)
}

// unconfirmedInsert handles a failure after an Insert that may have been
// accepted. An *operationError (only reported once DONE) means nothing was
// created, so err is returned as-is with no instance. Anything else is an
// unknown outcome: return the instance's identity with an
// *insertUnconfirmedError.
func (p *provider) unconfirmedInsert(opts autoscaler.InstanceCreateOpts, name, zone, size string, err error) (*autoscaler.Instance, error) {
	var opErr *operationError
	if errors.As(err, &opErr) {
		return nil, err
	}
	return &autoscaler.Instance{
		Provider:            autoscaler.ProviderGoogle,
		ID:                  name,
		Name:                opts.Name,
		Image:               p.image,
		Region:              zone,
		Size:                size,
		ServiceAccountEmail: p.serviceAccountEmail,
		Scopes:              p.scopes,
	}, &insertUnconfirmedError{Err: err}
}

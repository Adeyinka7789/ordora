// Package flags implements Waffle-style feature flags: platform-global
// kill switches with optional per-org percentage rollouts, plus
// per-org overrides that force a flag on or off for one tenant
// regardless of the global rule.
//
// Flags live in the feature_flags table (overrides in
// feature_flag_overrides) and are toggled from the admin panel. The
// Provider caches a snapshot in memory (refreshed on an interval;
// busted immediately on admin writes in-process) so evaluation
// never costs a database query per request.
//
// Evaluation is fail-closed: unknown flags and disabled flags are off.
// Rollouts are deterministic per org (hash of flag key + org id), so an
// org sees a stable experience as the percentage ramps up.
//
// NOTE: the in-process cache means flag changes can take up to the refresh
// interval to reach every instance when running multiple binaries. The
// admin write path calls Refresh immediately, which covers the single-
// binary deployment; a multi-instance setup should shorten the interval
// or add a pub/sub invalidation.
package flags

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Flag is one feature switch.
type Flag struct {
	Key            string
	Name           string
	Description    string
	Enabled        bool
	RolloutPercent int // 0-100
	UpdatedAt      time.Time
}

// Loader fetches the full flag set. Implemented by the postgres repo;
// kept as a func so this package stays dependency-free.
type Loader func(ctx context.Context) ([]Flag, error)

// Override forces a flag on or off for one org, winning over the global
// switch and rollout. Implemented by the postgres repo.
type Override struct {
	FlagKey string
	OrgID   uuid.UUID
	Enabled bool
}

// OverrideLoader fetches all per-org overrides. Nil means "no overrides".
type OverrideLoader func(ctx context.Context) ([]Override, error)

// Provider holds a cached snapshot and evaluates flags against it.
type Provider struct {
	mu             sync.RWMutex
	flags          map[string]Flag
	overrides      map[string]map[uuid.UUID]bool
	loader         Loader
	overrideLoader OverrideLoader
	interval       time.Duration
}

// NewProvider returns a Provider that refreshes every interval.
// Pass interval <= 0 to disable background refresh (tests, one-shots).
func NewProvider(loader Loader, interval time.Duration) *Provider {
	return &Provider{
		flags:    map[string]Flag{},
		loader:   loader,
		interval: interval,
	}
}

// WithOverrideLoader attaches the override source. Chain off NewProvider:
// flags.NewProvider(load, interval).WithOverrideLoader(loadOverrides).
func (p *Provider) WithOverrideLoader(l OverrideLoader) *Provider {
	p.overrideLoader = l
	return p
}

// Start loads the initial snapshot, then refreshes on the interval until
// ctx is done. Returns the initial load error, if any (the provider keeps
// serving the last good snapshot on later failures).
func (p *Provider) Start(ctx context.Context) error {
	if err := p.Refresh(ctx); err != nil {
		return err
	}
	if p.interval <= 0 {
		return nil
	}
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			_ = p.Refresh(ctx)
		}
	}
}

// Refresh reloads the snapshot now. Called by Start and by the admin write
// path so toggles take effect immediately in-process.
func (p *Provider) Refresh(ctx context.Context) error {
	list, err := p.loader(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]Flag, len(list))
	for _, f := range list {
		next[f.Key] = f
	}
	var over map[string]map[uuid.UUID]bool
	if p.overrideLoader != nil {
		rows, err := p.overrideLoader(ctx)
		if err != nil {
			return err
		}
		over = make(map[string]map[uuid.UUID]bool, len(rows))
		for _, o := range rows {
			m, ok := over[o.FlagKey]
			if !ok {
				m = map[uuid.UUID]bool{}
				over[o.FlagKey] = m
			}
			m[o.OrgID] = o.Enabled
		}
	}
	p.mu.Lock()
	p.flags = next
	p.overrides = over
	p.mu.Unlock()
	return nil
}

// Enabled reports whether key is on for the given org. Fail-closed:
// unknown keys, disabled flags, and 0% rollouts are off. A per-org
// override wins over everything else.
func (p *Provider) Enabled(orgID uuid.UUID, key string) bool {
	p.mu.RLock()
	overridden, hasOverride := p.overrides[key][orgID]
	f, ok := p.flags[key]
	p.mu.RUnlock()
	if hasOverride {
		return overridden
	}
	if !ok || !f.Enabled {
		return false
	}
	if f.RolloutPercent >= 100 {
		return true
	}
	if f.RolloutPercent <= 0 {
		return false
	}
	return rolloutBucket(key, orgID) < f.RolloutPercent
}

// Overridden reports whether the org has an explicit override for key,
// and its value. Admin display use.
func (p *Provider) Overridden(orgID uuid.UUID, key string) (enabled, ok bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	enabled, ok = p.overrides[key][orgID]
	return enabled, ok
}

// All returns a sorted copy of the snapshot (admin/debug use).
func (p *Provider) All() []Flag {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Flag, 0, len(p.flags))
	for _, f := range p.flags {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// rolloutBucket deterministically maps (key, org) to 0-99.
func rolloutBucket(key string, orgID uuid.UUID) int {
	sum := sha256.Sum256([]byte(key + ":" + orgID.String()))
	return int(binary.BigEndian.Uint64(sum[:8]) % 100)
}

// Package samplingrate provides sampling rate storage backed by FlowStore.
package samplingrate

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/pkg/flowstore"
)

type flowStoreSamplingRateKey struct {
	RouterKey   string
	Version     uint16
	ObsDomainID uint32
}

// ErrNotFound is returned when a sampling rate entry is absent.
var ErrNotFound = errors.New("sampling rate not found")

// Store describes sampling rate storage keyed by router/version/obs-domain.
type Store interface {
	Set(ctx netflow.FlowContext, version uint16, obsDomainId uint32, rate uint32) error
	Get(ctx netflow.FlowContext, version uint16, obsDomainId uint32) (uint32, bool, error)
	Remove(ctx netflow.FlowContext, version uint16, obsDomainId uint32) (uint32, bool, error)
	GetAll() map[string]map[uint64]uint32
	Start()
	Close()
}

// Hooks receives sampling rate lifecycle events.
type Hooks struct {
	OnSet    func(router string, version uint16, obsDomainId uint32, rate uint32, existed bool) // called after Set
	OnAccess func(router string, version uint16, obsDomainId uint32, rate uint32)               // called after Get
	OnRemove func(router string, version uint16, obsDomainId uint32, rate uint32)               // called after Remove/expiry
}

// ComposeHooks combines multiple sampling-rate hook sets into one.
func ComposeHooks(hooks ...Hooks) Hooks {
	var combined Hooks
	for _, hookSet := range hooks {
		if hookSet.OnSet != nil {
			prev := combined.OnSet
			next := hookSet.OnSet
			combined.OnSet = func(router string, version uint16, obsDomainId uint32, rate uint32, existed bool) {
				if prev != nil {
					prev(router, version, obsDomainId, rate, existed)
				}
				next(router, version, obsDomainId, rate, existed)
			}
		}
		if hookSet.OnAccess != nil {
			prev := combined.OnAccess
			next := hookSet.OnAccess
			combined.OnAccess = func(router string, version uint16, obsDomainId uint32, rate uint32) {
				if prev != nil {
					prev(router, version, obsDomainId, rate)
				}
				next(router, version, obsDomainId, rate)
			}
		}
		if hookSet.OnRemove != nil {
			prev := combined.OnRemove
			next := hookSet.OnRemove
			combined.OnRemove = func(router string, version uint16, obsDomainId uint32, rate uint32) {
				if prev != nil {
					prev(router, version, obsDomainId, rate)
				}
				next(router, version, obsDomainId, rate)
			}
		}
	}
	return combined
}

// SamplingRateFlowStore implements Store using FlowStore with TTL and optional JSON persistence.
type SamplingRateFlowStore struct {
	lock           sync.RWMutex
	store          *flowstore.Store[flowStoreSamplingRateKey, uint32]
	ttl            time.Duration
	extendOnAccess bool
	sweepInterval  time.Duration
	now            func() time.Time
	closeOnce      sync.Once
	startOnce      sync.Once
	hooks          Hooks
	closeHooks     []func()
}

// FlowStoreOption configures SamplingRateFlowStore.
type FlowStoreOption func(*SamplingRateFlowStore)

// WithTTL sets the default TTL for sampling-rate entries. Zero disables expiry.
func WithTTL(ttl time.Duration) FlowStoreOption {
	return func(s *SamplingRateFlowStore) { s.ttl = ttl }
}

// WithExtendOnAccess refreshes the default TTL when entries are read.
func WithExtendOnAccess(enable bool) FlowStoreOption {
	return func(s *SamplingRateFlowStore) { s.extendOnAccess = enable }
}

// WithSweepInterval sets how often the underlying FlowStore runs expiry sweeps.
func WithSweepInterval(interval time.Duration) FlowStoreOption {
	return func(s *SamplingRateFlowStore) { s.sweepInterval = interval }
}

// WithHooks composes lifecycle hooks onto the store wrapper.
func WithHooks(hooks Hooks) FlowStoreOption {
	return func(s *SamplingRateFlowStore) { s.hooks = ComposeHooks(s.hooks, hooks) }
}

// WithNow overrides the clock used for TTL calculations. Intended for tests.
func WithNow(now func() time.Time) FlowStoreOption {
	return func(s *SamplingRateFlowStore) { s.now = now }
}

// WithCloseHook registers a callback run before the wrapped FlowStore is stopped.
func WithCloseHook(hook func()) FlowStoreOption {
	return func(s *SamplingRateFlowStore) {
		if hook != nil {
			s.closeHooks = append(s.closeHooks, hook)
		}
	}
}

// NewSamplingRateFlowStore builds a FlowStore-backed sampling rate store.
func NewSamplingRateFlowStore(opts ...FlowStoreOption) *SamplingRateFlowStore {
	s := &SamplingRateFlowStore{
		sweepInterval: time.Minute,
		now:           time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	storeOpts := []flowstore.StoreOption[flowStoreSamplingRateKey, uint32]{
		flowstore.WithRefreshTTLOnWrite[flowStoreSamplingRateKey, uint32](),
		flowstore.WithNow[flowStoreSamplingRateKey, uint32](s.now),
		flowstore.WithExpireHook[flowStoreSamplingRateKey, uint32](func(key flowStoreSamplingRateKey, val uint32) (bool, time.Duration) {
			return false, 0
		}),
	}
	if s.extendOnAccess {
		storeOpts = append(storeOpts, flowstore.WithRefreshTTLOnRead[flowStoreSamplingRateKey, uint32]())
	}
	if s.ttl > 0 {
		storeOpts = append(storeOpts, flowstore.WithDefaultTTL[flowStoreSamplingRateKey, uint32](s.ttl))
	}
	storeOpts = append(storeOpts, flowstore.WithHooks[flowStoreSamplingRateKey, uint32](s.buildStoreHooks()))

	s.store = flowstore.NewStore[flowStoreSamplingRateKey, uint32](storeOpts...)
	return s
}

// Start begins background expiry sweeps in the underlying FlowStore.
func (s *SamplingRateFlowStore) Start() {
	s.startOnce.Do(func() {
		s.store.Start(s.sweepInterval)
	})
}

// Close runs shutdown hooks, stops background expiry sweeps, and closes the error channel.
func (s *SamplingRateFlowStore) Close() {
	s.closeOnce.Do(func() {
		for _, hook := range s.closeHooks {
			hook()
		}
		s.store.Stop()
	})
}

// Set stores or replaces a sampling rate.
func (s *SamplingRateFlowStore) Set(ctx netflow.FlowContext, version uint16, obsDomainId uint32, rate uint32) error {
	key := s.buildKey(ctx, version, obsDomainId)
	if _, err := s.store.Set(key, rate); err != nil {
		return fmt.Errorf("sampling rate set %s %d/%d: %w", ctx.RouterKey, version, obsDomainId, err)
	}
	return nil
}

// Get retrieves a sampling rate.
func (s *SamplingRateFlowStore) Get(ctx netflow.FlowContext, version uint16, obsDomainId uint32) (uint32, bool, error) {
	key := s.buildKey(ctx, version, obsDomainId)
	var rate uint32
	if s.store.Get(key, &rate) {
		return rate, true, nil
	}
	return 0, false, nil
}

// Remove deletes a sampling rate entry.
func (s *SamplingRateFlowStore) Remove(ctx netflow.FlowContext, version uint16, obsDomainId uint32) (uint32, bool, error) {
	key := s.buildKey(ctx, version, obsDomainId)
	var rate uint32
	if !s.store.GetQuiet(key, &rate) {
		return 0, false, ErrNotFound
	}
	if s.store.Delete(key) {
		return rate, true, nil
	}
	return 0, false, ErrNotFound
}

// GetAll returns a snapshot of all sampling rates.
func (s *SamplingRateFlowStore) GetAll() map[string]map[uint64]uint32 {
	ret := make(map[string]map[uint64]uint32)
	s.store.Range(func(key flowStoreSamplingRateKey, val uint32) bool {
		router := key.RouterKey
		bucket := ret[router]
		if bucket == nil {
			bucket = make(map[uint64]uint32)
			ret[router] = bucket
		}
		bucket[composeSamplingKey(key.Version, key.ObsDomainID)] = val
		return true
	})
	return ret
}

// buildStoreHooks adapts sampling-rate hooks onto the generic FlowStore hook API.
func (s *SamplingRateFlowStore) buildStoreHooks() flowstore.Hooks[flowStoreSamplingRateKey, uint32] {
	s.lock.RLock()
	hookSet := s.hooks
	s.lock.RUnlock()

	var hooks flowstore.Hooks[flowStoreSamplingRateKey, uint32]
	if hookSet.OnSet != nil {
		hooks.OnSet = func(key flowStoreSamplingRateKey, value uint32, existed bool) {
			hookSet.OnSet(key.RouterKey, key.Version, key.ObsDomainID, value, existed)
		}
	}
	if hookSet.OnAccess != nil {
		hooks.OnGet = func(key flowStoreSamplingRateKey, value uint32) {
			hookSet.OnAccess(key.RouterKey, key.Version, key.ObsDomainID, value)
		}
	}
	if hookSet.OnRemove != nil {
		hooks.OnDelete = func(key flowStoreSamplingRateKey, value uint32, _ flowstore.DeleteReason) {
			hookSet.OnRemove(key.RouterKey, key.Version, key.ObsDomainID, value)
		}
	}
	return hooks
}

// buildKey converts the decoder-facing routing tuple into the internal FlowStore key.
func (s *SamplingRateFlowStore) buildKey(ctx netflow.FlowContext, version uint16, obsDomainId uint32) flowStoreSamplingRateKey {
	return flowStoreSamplingRateKey{
		RouterKey:   ctx.RouterKey,
		Version:     version,
		ObsDomainID: obsDomainId,
	}
}

// composeSamplingKey packs version and observation domain for snapshot output.
func composeSamplingKey(version uint16, obsDomainId uint32) uint64 {
	return (uint64(version) << 32) | uint64(obsDomainId)
}

// decodeSamplingKey unpacks a snapshot key produced by composeSamplingKey.
func decodeSamplingKey(key uint64) (uint16, uint32) {
	version := uint16(key >> 32)
	obsDomainId := uint32(key & 0xFFFFFFFF)
	return version, obsDomainId
}

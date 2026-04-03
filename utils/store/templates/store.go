// Package templates provides NetFlow/IPFIX template storage backed by FlowStore.
package templates

import (
	"fmt"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/pkg/flowstore"
)

type flowStoreTemplateKey struct {
	RouterKey   string
	Version     uint16
	ObsDomainID uint32
	TemplateID  uint16
}

// TemplateFlowStore implements netflow.ManagedTemplateStore using FlowStore.
type TemplateFlowStore struct {
	lock           sync.RWMutex
	store          *flowstore.Store[flowStoreTemplateKey, interface{}]
	ttl            time.Duration
	extendOnAccess bool
	sweepInterval  time.Duration
	now            func() time.Time
	closeOnce      sync.Once
	startOnce      sync.Once
	hooks          TemplateHooks
	closeHooks     []func()
}

// TemplateHooks receives template lifecycle events.
type TemplateHooks struct {
	OnAdd    func(router string, version uint16, obsDomainId uint32, templateId uint16, template interface{}, updated bool)
	OnAccess func(router string, version uint16, obsDomainId uint32, templateId uint16, template interface{})
	OnRemove func(router string, version uint16, obsDomainId uint32, templateId uint16, template interface{})
}

// ComposeHooks combines multiple template hook sets into one.
func ComposeHooks(hooks ...TemplateHooks) TemplateHooks {
	var combined TemplateHooks
	for _, hookSet := range hooks {
		if hookSet.OnAdd != nil {
			prev := combined.OnAdd
			next := hookSet.OnAdd
			combined.OnAdd = func(router string, version uint16, obsDomainId uint32, templateId uint16, template interface{}, updated bool) {
				if prev != nil {
					prev(router, version, obsDomainId, templateId, template, updated)
				}
				next(router, version, obsDomainId, templateId, template, updated)
			}
		}
		if hookSet.OnAccess != nil {
			prev := combined.OnAccess
			next := hookSet.OnAccess
			combined.OnAccess = func(router string, version uint16, obsDomainId uint32, templateId uint16, template interface{}) {
				if prev != nil {
					prev(router, version, obsDomainId, templateId, template)
				}
				next(router, version, obsDomainId, templateId, template)
			}
		}
		if hookSet.OnRemove != nil {
			prev := combined.OnRemove
			next := hookSet.OnRemove
			combined.OnRemove = func(router string, version uint16, obsDomainId uint32, templateId uint16, template interface{}) {
				if prev != nil {
					prev(router, version, obsDomainId, templateId, template)
				}
				next(router, version, obsDomainId, templateId, template)
			}
		}
	}
	return combined
}

// FlowStoreOption configures a TemplateFlowStore.
type FlowStoreOption func(*TemplateFlowStore)

// WithTTL sets the default TTL for template entries. Zero disables expiry.
func WithTTL(ttl time.Duration) FlowStoreOption {
	return func(s *TemplateFlowStore) { s.ttl = ttl }
}

// WithExtendOnAccess refreshes the default TTL when templates are read.
func WithExtendOnAccess(enable bool) FlowStoreOption {
	return func(s *TemplateFlowStore) { s.extendOnAccess = enable }
}

// WithSweepInterval sets how often the underlying FlowStore runs expiry sweeps.
func WithSweepInterval(interval time.Duration) FlowStoreOption {
	return func(s *TemplateFlowStore) { s.sweepInterval = interval }
}

// WithHooks composes lifecycle hooks onto the template store wrapper.
func WithHooks(hooks TemplateHooks) FlowStoreOption {
	return func(s *TemplateFlowStore) { s.hooks = ComposeHooks(s.hooks, hooks) }
}

// WithNow overrides the clock used for TTL calculations. Intended for tests.
func WithNow(now func() time.Time) FlowStoreOption {
	return func(s *TemplateFlowStore) { s.now = now }
}

// WithCloseHook registers a callback run before the wrapped FlowStore is stopped.
func WithCloseHook(hook func()) FlowStoreOption {
	return func(s *TemplateFlowStore) {
		if hook != nil {
			s.closeHooks = append(s.closeHooks, hook)
		}
	}
}

// NewTemplateFlowStore creates a FlowStore-backed template store.
func NewTemplateFlowStore(opts ...FlowStoreOption) *TemplateFlowStore {
	s := &TemplateFlowStore{
		sweepInterval: time.Minute,
		now:           time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	storeOpts := []flowstore.StoreOption[flowStoreTemplateKey, interface{}]{
		flowstore.WithRefreshTTLOnWrite[flowStoreTemplateKey, interface{}](),
		flowstore.WithNow[flowStoreTemplateKey, interface{}](s.now),
		flowstore.WithExpireHook[flowStoreTemplateKey, interface{}](func(key flowStoreTemplateKey, _ interface{}) (bool, time.Duration) {
			return false, 0
		}),
	}
	if s.extendOnAccess {
		storeOpts = append(storeOpts, flowstore.WithRefreshTTLOnRead[flowStoreTemplateKey, interface{}]())
	}
	if s.ttl > 0 {
		storeOpts = append(storeOpts, flowstore.WithDefaultTTL[flowStoreTemplateKey, interface{}](s.ttl))
	}
	storeOpts = append(storeOpts, flowstore.WithHooks[flowStoreTemplateKey, interface{}](s.buildStoreHooks()))

	s.store = flowstore.NewStore[flowStoreTemplateKey, interface{}](storeOpts...)
	return s
}

// Start begins background expiry sweeps in the underlying FlowStore.
func (s *TemplateFlowStore) Start() {
	s.startOnce.Do(func() {
		s.store.Start(s.sweepInterval)
	})
}

// Close runs shutdown hooks, stops background expiry sweeps, and closes the error channel.
func (s *TemplateFlowStore) Close() {
	s.closeOnce.Do(func() {
		for _, hook := range s.closeHooks {
			hook()
		}
		s.store.Stop()
	})
}

// AddTemplate stores or replaces one template and reports whether it was added or updated.
func (s *TemplateFlowStore) AddTemplate(ctx netflow.FlowContext, version uint16, obsDomainId uint32, templateId uint16, template interface{}) (netflow.TemplateStatus, error) {
	key := flowStoreTemplateKey{
		RouterKey:   ctx.RouterKey,
		Version:     version,
		ObsDomainID: obsDomainId,
		TemplateID:  templateId,
	}
	var existing interface{}
	exists := s.store.GetQuiet(key, &existing)
	if _, err := s.store.Set(key, template); err != nil {
		return netflow.TemplateUnchanged, fmt.Errorf("flowstore templates add %s %d/%d/%d: %w", ctx.RouterKey, version, obsDomainId, templateId, err)
	}
	if exists {
		return netflow.TemplateUpdated, nil
	}
	return netflow.TemplateAdded, nil
}

// GetTemplate returns one template by router/version/domain/template id.
func (s *TemplateFlowStore) GetTemplate(ctx netflow.FlowContext, version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	key := flowStoreTemplateKey{
		RouterKey:   ctx.RouterKey,
		Version:     version,
		ObsDomainID: obsDomainId,
		TemplateID:  templateId,
	}
	var template interface{}
	if s.store.Get(key, &template) {
		return template, nil
	}
	return nil, netflow.ErrorTemplateNotFound
}

// RemoveTemplate deletes one template and returns the previous value when present.
func (s *TemplateFlowStore) RemoveTemplate(ctx netflow.FlowContext, version uint16, obsDomainId uint32, templateId uint16) (interface{}, bool, error) {
	key := flowStoreTemplateKey{
		RouterKey:   ctx.RouterKey,
		Version:     version,
		ObsDomainID: obsDomainId,
		TemplateID:  templateId,
	}
	var template interface{}
	if !s.store.GetQuiet(key, &template) {
		return nil, false, netflow.ErrorTemplateNotFound
	}
	if s.store.Delete(key) {
		return template, true, nil
	}
	return nil, false, netflow.ErrorTemplateNotFound
}

// GetAll returns a snapshot of all templates grouped by router key.
func (s *TemplateFlowStore) GetAll() map[string]netflow.FlowBaseTemplateSet {
	ret := make(map[string]netflow.FlowBaseTemplateSet)
	s.store.Range(func(key flowStoreTemplateKey, val interface{}) bool {
		router := key.RouterKey
		bucket := ret[router]
		if bucket == nil {
			bucket = make(netflow.FlowBaseTemplateSet)
			ret[router] = bucket
		}
		bucket[composeTemplateKey(key.Version, key.ObsDomainID, key.TemplateID)] = val
		return true
	})
	return ret
}

// buildStoreHooks adapts template hooks onto the generic FlowStore hook API.
func (s *TemplateFlowStore) buildStoreHooks() flowstore.Hooks[flowStoreTemplateKey, interface{}] {
	s.lock.RLock()
	hookSet := s.hooks
	s.lock.RUnlock()

	var hooks flowstore.Hooks[flowStoreTemplateKey, interface{}]
	if hookSet.OnAdd != nil {
		hooks.OnSet = func(key flowStoreTemplateKey, value interface{}, existed bool) {
			hookSet.OnAdd(key.RouterKey, key.Version, key.ObsDomainID, key.TemplateID, value, existed)
		}
	}
	if hookSet.OnAccess != nil {
		hooks.OnGet = func(key flowStoreTemplateKey, value interface{}) {
			hookSet.OnAccess(key.RouterKey, key.Version, key.ObsDomainID, key.TemplateID, value)
		}
	}
	if hookSet.OnRemove != nil {
		hooks.OnDelete = func(key flowStoreTemplateKey, value interface{}, _ flowstore.DeleteReason) {
			hookSet.OnRemove(key.RouterKey, key.Version, key.ObsDomainID, key.TemplateID, value)
		}
	}
	return hooks
}

// composeTemplateKey packs version, observation domain, and template id for snapshots.
func composeTemplateKey(version uint16, obsDomainId uint32, templateId uint16) uint64 {
	return (uint64(version) << 48) | (uint64(obsDomainId) << 16) | uint64(templateId)
}

// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"sync"
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

// TemplateKey identifies a template entry for expiry tracking.
type TemplateKey struct {
	Version     uint16
	ObsDomainID uint32
	TemplateID  uint16
}

// ExpirableSystem adds expiry controls to a template system.
type ExpirableSystem interface {
	netflow.NetFlowTemplateSystem
	ExpireBefore(cutoff time.Time) int
	ExpireStale() int
}

// ExpiringTemplateSystem tracks template update times and supports expiry.
type ExpiringTemplateSystem struct {
	wrapped        netflow.NetFlowTemplateSystem
	lock           sync.Mutex
	updated        map[TemplateKey]time.Time
	ttl            time.Duration
	now            func() time.Time
	key            string
	reg            *ExpiringRegistry
	extendOnAccess bool
}

// NewExpiringTemplateSystem wraps a template system with expiry tracking.
func NewExpiringTemplateSystem(wrapped netflow.NetFlowTemplateSystem, ttl time.Duration) *ExpiringTemplateSystem {
	if wrapped == nil {
		wrapped = netflow.CreateTemplateSystem()
	}
	return &ExpiringTemplateSystem{
		wrapped: wrapped,
		updated: make(map[TemplateKey]time.Time),
		ttl:     ttl,
		now:     time.Now,
	}
}

// AddTemplate records template update time and forwards to the wrapped system.
func (s *ExpiringTemplateSystem) AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) (netflow.TemplateStatus, error) {
	update, err := s.wrapped.AddTemplate(version, obsDomainId, templateId, template)
	if err != nil {
		return update, err
	}
	s.lock.Lock()
	s.updated[TemplateKey{Version: version, ObsDomainID: obsDomainId, TemplateID: templateId}] = s.now()
	s.lock.Unlock()
	if s.reg != nil {
		if update == netflow.TemplateAdded {
			s.reg.increment(s.key)
		}
	}
	return update, nil
}

// SetExtendOnAccess toggles whether accesses refresh the template's expiry time.
func (s *ExpiringTemplateSystem) SetExtendOnAccess(enable bool) {
	s.lock.Lock()
	s.extendOnAccess = enable
	s.lock.Unlock()
}

// GetTemplate forwards template lookup to the wrapped system.
func (s *ExpiringTemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	template, err := s.wrapped.GetTemplate(version, obsDomainId, templateId)
	if err != nil {
		return template, err
	}
	s.lock.Lock()
	if s.extendOnAccess {
		s.updated[TemplateKey{Version: version, ObsDomainID: obsDomainId, TemplateID: templateId}] = s.now()
	}
	s.lock.Unlock()
	return template, nil
}

// RemoveTemplate removes a template and its tracking entry.
func (s *ExpiringTemplateSystem) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, bool, error) {
	template, removed, err := s.wrapped.RemoveTemplate(version, obsDomainId, templateId)
	if removed {
		s.lock.Lock()
		delete(s.updated, TemplateKey{Version: version, ObsDomainID: obsDomainId, TemplateID: templateId})
		s.lock.Unlock()
		if s.reg != nil {
			s.reg.decrement(s.key)
		}
	}
	return template, removed, err
}

// GetTemplates returns all templates from the wrapped system.
func (s *ExpiringTemplateSystem) GetTemplates() netflow.FlowBaseTemplateSet {
	return s.wrapped.GetTemplates()
}

// ExpireBefore removes templates last updated before the cutoff.
func (s *ExpiringTemplateSystem) ExpireBefore(cutoff time.Time) int {
	var expired []TemplateKey

	s.lock.Lock()
	for key, updated := range s.updated {
		if updated.Before(cutoff) {
			expired = append(expired, key)
		}
	}
	s.lock.Unlock()

	removed := 0
	for _, key := range expired {
		if _, removedTemplate, err := s.RemoveTemplate(key.Version, key.ObsDomainID, key.TemplateID); err == nil && removedTemplate {
			removed++
		}
	}
	return removed
}

// ExpireStale removes templates older than the configured TTL.
func (s *ExpiringTemplateSystem) ExpireStale() int {
	if s.ttl <= 0 {
		return 0
	}
	return s.ExpireBefore(s.now().Add(-s.ttl))
}

// ExpiringRegistry provides expiry controls for all router template systems.
type ExpiringRegistry struct {
	lock           sync.RWMutex
	wrapped        Registry
	systems        map[string]*ExpiringTemplateSystem
	counts         map[string]int
	emptySince     map[string]time.Time
	ttl            time.Duration
	emptyTTL       time.Duration
	now            func() time.Time
	extendOnAccess bool
	sweeperLock    sync.Mutex
	sweeperStop    chan struct{}
	sweeperDone    chan struct{}
	closeOnce      sync.Once
}

// NewExpiringRegistry wraps a registry with expiry tracking.
func NewExpiringRegistry(wrapped Registry, ttl time.Duration) *ExpiringRegistry {
	if wrapped == nil {
		wrapped = NewInMemoryRegistry(nil)
	}
	return &ExpiringRegistry{
		wrapped: wrapped,
		systems: make(map[string]*ExpiringTemplateSystem),
		counts:  make(map[string]int),
		emptySince: make(map[string]time.Time),
		ttl:     ttl,
		emptyTTL: ttl,
		now:     time.Now,
	}
}

// GetSystem returns a wrapped template system for a router.
func (r *ExpiringRegistry) GetSystem(key string) netflow.NetFlowTemplateSystem {
	r.lock.RLock()
	system, ok := r.systems[key]
	r.lock.RUnlock()
	if ok {
		r.lock.Lock()
		if r.counts[key] == 0 {
			r.emptySince[key] = r.now()
		}
		r.lock.Unlock()
		return system
	}

	wrapped := r.wrapped.GetSystem(key)
	system = NewExpiringTemplateSystem(wrapped, r.ttl)
	system.now = r.now
	system.key = key
	system.reg = r
	system.extendOnAccess = r.extendOnAccess

	r.lock.Lock()
	if existing, ok := r.systems[key]; ok {
		r.lock.Unlock()
		return existing
	}
	r.systems[key] = system
	count := len(wrapped.GetTemplates())
	r.counts[key] = count
	if count == 0 {
		r.emptySince[key] = r.now()
	} else {
		delete(r.emptySince, key)
	}
	r.lock.Unlock()
	return system
}

// SetExtendOnAccess toggles whether template accesses refresh their expiry time.
func (r *ExpiringRegistry) SetExtendOnAccess(enable bool) {
	r.lock.Lock()
	r.extendOnAccess = enable
	systems := make([]*ExpiringTemplateSystem, 0, len(r.systems))
	for _, system := range r.systems {
		systems = append(systems, system)
	}
	r.lock.Unlock()
	for _, system := range systems {
		system.SetExtendOnAccess(enable)
	}
}

// GetAll returns all templates for every router.
func (r *ExpiringRegistry) GetAll() map[string]netflow.FlowBaseTemplateSet {
	r.lock.RLock()
	systems := make(map[string]netflow.NetFlowTemplateSystem, len(r.systems))
	for key, system := range r.systems {
		systems[key] = system
	}
	r.lock.RUnlock()

	ret := make(map[string]netflow.FlowBaseTemplateSet, len(systems))
	for key, system := range systems {
		ret[key] = system.GetTemplates()
	}
	return ret
}

func (r *ExpiringRegistry) increment(key string) {
	r.lock.Lock()
	count := r.counts[key] + 1
	r.counts[key] = count
	if count == 1 {
		delete(r.emptySince, key)
	}
	r.lock.Unlock()
}

func (r *ExpiringRegistry) decrement(key string) int {
	r.lock.Lock()
	count := r.counts[key] - 1
	if count < 0 {
		count = 0
	}
	r.counts[key] = count
	if count == 0 {
		r.emptySince[key] = r.now()
	}
	r.lock.Unlock()
	return count
}

func (r *ExpiringRegistry) pruneEmptyBefore(cutoff time.Time) {
	var keys []string
	r.lock.Lock()
	for key, since := range r.emptySince {
		if since.Before(cutoff) {
			keys = append(keys, key)
			delete(r.emptySince, key)
			delete(r.counts, key)
			delete(r.systems, key)
		}
	}
	r.lock.Unlock()
	if len(keys) == 0 {
		return
	}
	if remover, ok := r.wrapped.(interface{ RemoveSystem(string) }); ok {
		for _, key := range keys {
			remover.RemoveSystem(key)
		}
	}
}

// ExpireBefore removes templates older than the cutoff across all routers.
func (r *ExpiringRegistry) ExpireBefore(cutoff time.Time) int {
	r.lock.RLock()
	systems := make([]struct {
		key    string
		system *ExpiringTemplateSystem
	}, 0, len(r.systems))
	for key, system := range r.systems {
		systems = append(systems, struct {
			key    string
			system *ExpiringTemplateSystem
		}{
			key:    key,
			system: system,
		})
	}
	r.lock.RUnlock()

	removed := 0
	for _, entry := range systems {
		removed += entry.system.ExpireBefore(cutoff)
	}
	if r.emptyTTL > 0 {
		r.pruneEmptyBefore(cutoff)
	}
	return removed
}

// ExpireStale removes templates older than the configured TTL across all routers.
func (r *ExpiringRegistry) ExpireStale() int {
	if r.ttl <= 0 {
		return 0
	}
	return r.ExpireBefore(r.now().Add(-r.ttl))
}

// StartSweeper begins periodic expiry using the configured TTL.
func (r *ExpiringRegistry) StartSweeper(interval time.Duration) {
	if interval <= 0 {
		return
	}

	r.sweeperLock.Lock()
	if r.sweeperStop != nil {
		r.sweeperLock.Unlock()
		return
	}
	r.sweeperStop = make(chan struct{})
	r.sweeperDone = make(chan struct{})
	stop := r.sweeperStop
	done := r.sweeperDone
	r.sweeperLock.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer close(done)
		for {
			select {
			case <-ticker.C:
				r.ExpireStale()
			case <-stop:
				return
			}
		}
	}()
}

// Close stops the sweeper goroutine if it is running.
func (r *ExpiringRegistry) Close() {
	r.closeOnce.Do(func() {
		r.sweeperLock.Lock()
		if r.sweeperStop == nil {
			r.sweeperLock.Unlock()
			r.wrapped.Close()
			return
		}
		stop := r.sweeperStop
		done := r.sweeperDone
		r.sweeperStop = nil
		r.sweeperDone = nil
		r.sweeperLock.Unlock()

		close(stop)
		<-done
		r.wrapped.Close()
	})
}

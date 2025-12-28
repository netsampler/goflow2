// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"sync"
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

// SweepingRegistry allows starting a periodic expiry sweeper.
type SweepingRegistry interface {
	StartSweeper(interval time.Duration)
}

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
	wrapped netflow.NetFlowTemplateSystem
	lock    *sync.Mutex
	updated map[TemplateKey]time.Time
	ttl     time.Duration
	now     func() time.Time
	key     string
	reg     *ExpiringRegistry
}

// NewExpiringTemplateSystem wraps a template system with expiry tracking.
func NewExpiringTemplateSystem(wrapped netflow.NetFlowTemplateSystem, ttl time.Duration) *ExpiringTemplateSystem {
	if wrapped == nil {
		wrapped = netflow.CreateTemplateSystem()
	}
	return &ExpiringTemplateSystem{
		wrapped: wrapped,
		lock:    &sync.Mutex{},
		updated: make(map[TemplateKey]time.Time),
		ttl:     ttl,
		now:     time.Now,
	}
}

// AddTemplate records template update time and forwards to the wrapped system.
func (s *ExpiringTemplateSystem) AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) error {
	if err := s.wrapped.AddTemplate(version, obsDomainId, templateId, template); err != nil {
		return err
	}
	s.lock.Lock()
	s.updated[TemplateKey{Version: version, ObsDomainID: obsDomainId, TemplateID: templateId}] = s.now()
	s.lock.Unlock()
	if s.reg != nil {
		s.reg.increment(s.key)
	}
	return nil
}

// GetTemplate forwards template lookup to the wrapped system.
func (s *ExpiringTemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	return s.wrapped.GetTemplate(version, obsDomainId, templateId)
}

// RemoveTemplate removes a template and its tracking entry.
func (s *ExpiringTemplateSystem) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	template, err := s.wrapped.RemoveTemplate(version, obsDomainId, templateId)
	if err == nil {
		s.lock.Lock()
		delete(s.updated, TemplateKey{Version: version, ObsDomainID: obsDomainId, TemplateID: templateId})
		s.lock.Unlock()
		if s.reg != nil {
			s.reg.decrement(s.key)
		}
	}
	return template, err
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
		if _, err := s.RemoveTemplate(key.Version, key.ObsDomainID, key.TemplateID); err == nil {
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
	lock        *sync.Mutex
	wrapped     Registry
	systems     map[string]*ExpiringTemplateSystem
	counts      map[string]int
	ttl         time.Duration
	now         func() time.Time
	sweeperLock *sync.Mutex
	sweeperStop chan struct{}
	sweeperDone chan struct{}
	closeOnce   sync.Once
}

// NewExpiringRegistry wraps a registry with expiry tracking.
func NewExpiringRegistry(wrapped Registry, ttl time.Duration) *ExpiringRegistry {
	if wrapped == nil {
		wrapped = NewInMemoryRegistry(nil)
	}
	return &ExpiringRegistry{
		lock:        &sync.Mutex{},
		wrapped:     wrapped,
		systems:     make(map[string]*ExpiringTemplateSystem),
		counts:      make(map[string]int),
		ttl:         ttl,
		now:         time.Now,
		sweeperLock: &sync.Mutex{},
	}
}

// GetSystem returns a wrapped template system for a router.
func (r *ExpiringRegistry) GetSystem(key string) netflow.NetFlowTemplateSystem {
	r.lock.Lock()
	system, ok := r.systems[key]
	r.lock.Unlock()
	if ok {
		return system
	}

	wrapped := r.wrapped.GetSystem(key)
	system = NewExpiringTemplateSystem(wrapped, r.ttl)
	system.now = r.now
	system.key = key
	system.reg = r

	r.lock.Lock()
	if existing, ok := r.systems[key]; ok {
		r.lock.Unlock()
		return existing
	}
	r.systems[key] = system
	r.counts[key] = 0
	r.lock.Unlock()
	return system
}

// GetAll returns all templates for every router.
func (r *ExpiringRegistry) GetAll() map[string]netflow.FlowBaseTemplateSet {
	r.lock.Lock()
	systems := make(map[string]netflow.NetFlowTemplateSystem, len(r.systems))
	for key, system := range r.systems {
		systems[key] = system
	}
	r.lock.Unlock()

	ret := make(map[string]netflow.FlowBaseTemplateSet, len(systems))
	for key, system := range systems {
		ret[key] = system.GetTemplates()
	}
	return ret
}

func (r *ExpiringRegistry) increment(key string) {
	r.lock.Lock()
	r.counts[key]++
	r.lock.Unlock()
}

func (r *ExpiringRegistry) decrement(key string) int {
	r.lock.Lock()
	count := r.counts[key] - 1
	if count <= 0 {
		delete(r.counts, key)
		count = 0
		delete(r.systems, key)
	} else {
		r.counts[key] = count
	}
	r.lock.Unlock()
	return count
}

// ExpireBefore removes templates older than the cutoff across all routers.
func (r *ExpiringRegistry) ExpireBefore(cutoff time.Time) int {
	r.lock.Lock()
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
	r.lock.Unlock()

	removed := 0
	for _, entry := range systems {
		removed += entry.system.ExpireBefore(cutoff)
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

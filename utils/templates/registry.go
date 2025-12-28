// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"sync"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

// Registry provides access to per-router template systems.
type Registry interface {
	GetSystem(key string) netflow.NetFlowTemplateSystem
	GetAll() map[string]netflow.FlowBaseTemplateSet
	Close()
}

// InMemoryRegistry stores template systems in-memory keyed by router.
type InMemoryRegistry struct {
	lock      sync.RWMutex
	systems   map[string]netflow.NetFlowTemplateSystem
	generator TemplateSystemGenerator
}

type pruningTemplateSystem struct {
	key     string
	parent  *InMemoryRegistry
	wrapped netflow.NetFlowTemplateSystem
	lock    sync.Mutex
	count   int
}

func (s *pruningTemplateSystem) AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) (netflow.TemplateStatus, error) {
	update, err := s.wrapped.AddTemplate(version, obsDomainId, templateId, template)
	if err != nil {
		return update, err
	}
	s.lock.Lock()
	if update == netflow.TemplateAdded {
		s.count++
	}
	s.lock.Unlock()
	return update, nil
}

func (s *pruningTemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	return s.wrapped.GetTemplate(version, obsDomainId, templateId)
}

func (s *pruningTemplateSystem) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, bool, error) {
	template, removed, err := s.wrapped.RemoveTemplate(version, obsDomainId, templateId)
	if removed {
		var shouldPrune bool
		s.lock.Lock()
		if s.count > 0 {
			s.count--
		}
		if s.count == 0 {
			shouldPrune = true
		}
		s.lock.Unlock()
		if shouldPrune {
			s.parent.lock.Lock()
			delete(s.parent.systems, s.key)
			s.parent.lock.Unlock()
		}
	}
	return template, removed, err
}

func (s *pruningTemplateSystem) GetTemplates() netflow.FlowBaseTemplateSet {
	return s.wrapped.GetTemplates()
}

// NewInMemoryRegistry creates a registry with an optional system generator.
func NewInMemoryRegistry(generator TemplateSystemGenerator) *InMemoryRegistry {
	if generator == nil {
		generator = DefaultTemplateGenerator
	}
	return &InMemoryRegistry{
		systems:   make(map[string]netflow.NetFlowTemplateSystem),
		generator: generator,
	}
}

// GetSystem returns the template system for a router, creating it if needed.
func (r *InMemoryRegistry) GetSystem(key string) netflow.NetFlowTemplateSystem {
	r.lock.RLock()
	system, ok := r.systems[key]
	r.lock.RUnlock()
	if ok {
		return system
	}

	pruningSystem := &pruningTemplateSystem{
		key:     key,
		parent:  r,
		wrapped: r.generator(key),
	}
	if snapshot := pruningSystem.wrapped.GetTemplates(); len(snapshot) > 0 {
		pruningSystem.lock.Lock()
		pruningSystem.count = len(snapshot)
		pruningSystem.lock.Unlock()
	}
	system = pruningSystem
	r.lock.Lock()
	if existing, ok := r.systems[key]; ok {
		r.lock.Unlock()
		return existing
	}
	r.systems[key] = system
	r.lock.Unlock()
	return system
}

// GetAll returns a copy of templates for all known routers.
func (r *InMemoryRegistry) GetAll() map[string]netflow.FlowBaseTemplateSet {
	r.lock.RLock()
	defer r.lock.RUnlock()

	ret := make(map[string]netflow.FlowBaseTemplateSet, len(r.systems))
	for key, system := range r.systems {
		ret[key] = system.GetTemplates()
	}
	return ret
}

// Close is a no-op for the in-memory registry.
func (r *InMemoryRegistry) Close() {
}

// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"sync"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

// PruningRegistry removes router entries when their template set becomes empty.
type PruningRegistry struct {
	lock    *sync.Mutex
	wrapped Registry
	systems map[string]netflow.NetFlowTemplateSystem
}

// NewPruningRegistry wraps a registry to prune empty template systems.
func NewPruningRegistry(wrapped Registry) *PruningRegistry {
	if wrapped == nil {
		wrapped = NewInMemoryRegistry(nil)
	}
	return &PruningRegistry{
		lock:    &sync.Mutex{},
		wrapped: wrapped,
		systems: make(map[string]netflow.NetFlowTemplateSystem),
	}
}

// GetSystem returns a wrapped template system for a router.
func (r *PruningRegistry) GetSystem(key string) netflow.NetFlowTemplateSystem {
	r.lock.Lock()
	system, ok := r.systems[key]
	r.lock.Unlock()
	if ok {
		return system
	}

	wrapped := r.wrapped.GetSystem(key)
	system = &pruningTemplateSystem{
		key:     key,
		parent:  r,
		wrapped: wrapped,
	}

	r.lock.Lock()
	if existing, ok := r.systems[key]; ok {
		r.lock.Unlock()
		return existing
	}
	r.systems[key] = system
	r.lock.Unlock()
	return system
}

// GetAll returns all templates for every router.
func (r *PruningRegistry) GetAll() map[string]netflow.FlowBaseTemplateSet {
	return r.wrapped.GetAll()
}

// RemoveSystem deletes a router entry from the registry.
func (r *PruningRegistry) RemoveSystem(key string) bool {
	r.lock.Lock()
	_, ok := r.systems[key]
	if ok {
		delete(r.systems, key)
	}
	r.lock.Unlock()
	if prunable, ok := r.wrapped.(PrunableRegistry); ok {
		if prunable.RemoveSystem(key) {
			return true
		}
	}
	return ok
}

type pruningTemplateSystem struct {
	key     string
	parent  *PruningRegistry
	wrapped netflow.NetFlowTemplateSystem
}

func (s *pruningTemplateSystem) AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) error {
	return s.wrapped.AddTemplate(version, obsDomainId, templateId, template)
}

func (s *pruningTemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	return s.wrapped.GetTemplate(version, obsDomainId, templateId)
}

func (s *pruningTemplateSystem) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	template, err := s.wrapped.RemoveTemplate(version, obsDomainId, templateId)
	if err == nil && len(s.wrapped.GetTemplates()) == 0 {
		s.parent.RemoveSystem(s.key)
	}
	return template, err
}

func (s *pruningTemplateSystem) GetTemplates() netflow.FlowBaseTemplateSet {
	return s.wrapped.GetTemplates()
}

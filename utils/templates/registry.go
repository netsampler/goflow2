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

	system = r.generator(key)
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

// RemoveSystem deletes a router entry if present.
func (r *InMemoryRegistry) RemoveSystem(key string) {
	r.lock.Lock()
	delete(r.systems, key)
	r.lock.Unlock()
}

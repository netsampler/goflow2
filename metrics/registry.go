package metrics

import (
	"sync"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
	"github.com/netsampler/goflow2/v2/utils/templates"
)

// PromTemplateRegistry wraps a registry to record template metrics per router.
type PromTemplateRegistry struct {
	lock    *sync.Mutex
	wrapped templates.Registry
	systems map[string]netflow.NetFlowTemplateSystem
}

// NewDefaultPromTemplateRegistry creates a PromTemplateRegistry with default storage.
func NewDefaultPromTemplateRegistry() templates.Registry {
	return NewPromTemplateRegistry(templates.NewInMemoryRegistry(nil))
}

// NewPromTemplateRegistry wraps a registry and instruments its template systems.
func NewPromTemplateRegistry(wrapped templates.Registry) templates.Registry {
	if wrapped == nil {
		wrapped = templates.NewInMemoryRegistry(nil)
	}
	return &PromTemplateRegistry{
		lock:    &sync.Mutex{},
		wrapped: wrapped,
		systems: make(map[string]netflow.NetFlowTemplateSystem),
	}
}

// GetSystem returns a wrapped template system for a router.
func (r *PromTemplateRegistry) GetSystem(key string) netflow.NetFlowTemplateSystem {
	r.lock.Lock()
	system, ok := r.systems[key]
	r.lock.Unlock()
	if ok {
		return system
	}

	wrapped := r.wrapped.GetSystem(key)
	system = NewPromTemplateSystem(key, wrapped)

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
func (r *PromTemplateRegistry) GetAll() map[string]netflow.FlowBaseTemplateSet {
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

// Close forwards Close to the wrapped registry.
func (r *PromTemplateRegistry) Close() {
	r.wrapped.Close()
}

// RemoveSystem deletes a router entry if present.
func (r *PromTemplateRegistry) RemoveSystem(key string) {
	r.lock.Lock()
	delete(r.systems, key)
	r.lock.Unlock()
	r.wrapped.RemoveSystem(key)
}

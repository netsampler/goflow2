// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"sync"
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

// TemplateSystemRegistry owns per-source template systems and handles eviction.
// It lazily creates systems via the generator when new sources arrive.
type TemplateSystemRegistry struct {
	generator     TemplateSystemGenerator
	templates     map[string]netflow.NetFlowTemplateSystem
	lastSeen      map[string]time.Time
	lock          *sync.RWMutex
	evictAfter    time.Duration
	evictInterval time.Duration
	stopCh        chan struct{}
}

// NewTemplateSystemRegistry creates a registry with optional eviction.
func NewTemplateSystemRegistry(generator TemplateSystemGenerator, evictAfter, evictInterval time.Duration) *TemplateSystemRegistry {
	if generator == nil {
		generator = DefaultTemplateGenerator
	}
	manager := &TemplateSystemRegistry{
		generator:     generator,
		templates:     make(map[string]netflow.NetFlowTemplateSystem),
		lastSeen:      make(map[string]time.Time),
		lock:          &sync.RWMutex{},
		evictAfter:    evictAfter,
		evictInterval: evictInterval,
		stopCh:        make(chan struct{}),
	}
	if manager.evictAfter > 0 {
		if manager.evictInterval <= 0 {
			manager.evictInterval = time.Minute
		}
		go manager.evictLoop()
	}
	return manager
}

// Get returns the template system for a key, creating one if needed.
func (m *TemplateSystemRegistry) Get(key string) netflow.NetFlowTemplateSystem {
	now := time.Now()

	m.lock.RLock()
	templates, ok := m.templates[key]
	m.lock.RUnlock()
	if ok {
		m.touch(key, now)
		return templates
	}

	templates = m.generator(key)
	m.lock.Lock()
	m.templates[key] = templates
	m.lastSeen[key] = now
	m.lock.Unlock()
	return templates
}

// Remove deletes a template system for a key.
func (m *TemplateSystemRegistry) Remove(key string) {
	var tmpl netflow.NetFlowTemplateSystem
	m.lock.Lock()
	if existing, ok := m.templates[key]; ok {
		tmpl = existing
		delete(m.templates, key)
		delete(m.lastSeen, key)
	}
	m.lock.Unlock()
	if tmpl == nil {
		return
	}
	if cleaner, ok := tmpl.(interface {
		Cleanup()
	}); ok {
		cleaner.Cleanup()
	}
	if closer, ok := tmpl.(interface {
		Close() error
	}); ok {
		_ = closer.Close()
	}
}

// Close stops eviction and closes all template systems.
func (m *TemplateSystemRegistry) Close() {
	close(m.stopCh)
	m.lock.RLock()
	keys := make([]string, 0, len(m.templates))
	for key := range m.templates {
		keys = append(keys, key)
	}
	m.lock.RUnlock()
	for _, key := range keys {
		m.Remove(key)
	}
}

// Snapshot returns templates for all known sources.
func (m *TemplateSystemRegistry) Snapshot() map[string]netflow.FlowBaseTemplateSet {
	// Snapshot current template maps; callers expect a point-in-time view.
	m.lock.RLock()
	defer m.lock.RUnlock()
	ret := make(map[string]netflow.FlowBaseTemplateSet)
	for key, tmpl := range m.templates {
		ret[key] = tmpl.GetTemplates()
	}
	return ret
}

func (m *TemplateSystemRegistry) touch(key string, now time.Time) {
	m.lock.Lock()
	m.lastSeen[key] = now
	m.lock.Unlock()
}

func (m *TemplateSystemRegistry) evictLoop() {
	ticker := time.NewTicker(m.evictInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			// Periodic pass to evict idle sources or empty template sets.
			m.evictStale()
		}
	}
}

func (m *TemplateSystemRegistry) evictStale() {
	if m.evictAfter <= 0 {
		return
	}
	now := time.Now()
	var keys []string
	m.lock.RLock()
	for key, tmpl := range m.templates {
		seen := m.lastSeen[key]
		// Drop empty systems immediately; otherwise evict idle ones.
		if len(tmpl.GetTemplates()) == 0 {
			keys = append(keys, key)
			continue
		}
		if now.Sub(seen) > m.evictAfter {
			keys = append(keys, key)
		}
	}
	m.lock.RUnlock()
	for _, key := range keys {
		Remove(m, key)
	}
}

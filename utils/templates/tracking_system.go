// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"sync"
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

type templateTrackingSystem struct {
	wrapped  netflow.NetFlowTemplateSystem
	mu       sync.Mutex
	lastSeen map[uint64]time.Time
	onAccess func()
}

func newTemplateTrackingSystem(wrapped netflow.NetFlowTemplateSystem, onAccess func()) netflow.NetFlowTemplateSystem {
	if wrapped == nil {
		return wrapped
	}
	return &templateTrackingSystem{
		wrapped:  wrapped,
		lastSeen: make(map[uint64]time.Time),
		onAccess: onAccess,
	}
}

func templateKey(version uint16, obsDomainId uint32, templateId uint16) uint64 {
	return (uint64(version) << 48) | (uint64(obsDomainId) << 16) | uint64(templateId)
}

func (s *templateTrackingSystem) AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) error {
	if err := s.wrapped.AddTemplate(version, obsDomainId, templateId, template); err != nil {
		return err
	}
	s.touch(templateKey(version, obsDomainId, templateId))
	return nil
}

func (s *templateTrackingSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	template, err := s.wrapped.GetTemplate(version, obsDomainId, templateId)
	if err == nil {
		s.touch(templateKey(version, obsDomainId, templateId))
	}
	return template, err
}

func (s *templateTrackingSystem) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	template, err := s.wrapped.RemoveTemplate(version, obsDomainId, templateId)
	if err == nil {
		s.mu.Lock()
		delete(s.lastSeen, templateKey(version, obsDomainId, templateId))
		s.mu.Unlock()
		if s.onAccess != nil {
			s.onAccess()
		}
	}
	return template, err
}

func (s *templateTrackingSystem) GetTemplates() netflow.FlowBaseTemplateSet {
	return s.wrapped.GetTemplates()
}

func (s *templateTrackingSystem) Cleanup() {
	if cleaner, ok := s.wrapped.(interface {
		Cleanup()
	}); ok {
		cleaner.Cleanup()
	}
}

func (s *templateTrackingSystem) Close() error {
	if closer, ok := s.wrapped.(interface {
		Close() error
	}); ok {
		return closer.Close()
	}
	return nil
}

func (s *templateTrackingSystem) Flush() {
	if flusher, ok := s.wrapped.(interface {
		Flush()
	}); ok {
		flusher.Flush()
	}
}

func (s *templateTrackingSystem) touch(key uint64) {
	s.mu.Lock()
	s.lastSeen[key] = time.Now()
	s.mu.Unlock()
	if s.onAccess != nil {
		s.onAccess()
	}
}

func (s *templateTrackingSystem) EvictStaleTemplates(cutoff time.Time) int {
	s.mu.Lock()
	var keys []uint64
	for key, seen := range s.lastSeen {
		if seen.Before(cutoff) {
			keys = append(keys, key)
		}
	}
	s.mu.Unlock()

	evicted := 0
	for _, key := range keys {
		version := uint16(key >> 48)
		obsDomainID := uint32((key >> 16) & 0xffffffff)
		templateID := uint16(key & 0xffff)
		if _, err := s.RemoveTemplate(version, obsDomainID, templateID); err == nil {
			evicted++
		}
	}
	return evicted
}

// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

// JSONRegistry persists templates to a JSON file with batched updates.
type JSONRegistry struct {
	lock      *sync.Mutex
	wrapped   Registry
	systems   map[string]netflow.NetFlowTemplateSystem
	path      string
	interval  time.Duration
	changeCh  chan struct{}
	stopCh    chan struct{}
	doneCh    chan struct{}
	flushLock *sync.Mutex
	closeOnce sync.Once
}

// NewJSONRegistry wraps a registry and persists templates to a JSON file.
func NewJSONRegistry(path string, interval time.Duration, wrapped Registry) *JSONRegistry {
	if wrapped == nil {
		wrapped = NewInMemoryRegistry(nil)
	}
	r := &JSONRegistry{
		lock:      &sync.Mutex{},
		wrapped:   wrapped,
		systems:   make(map[string]netflow.NetFlowTemplateSystem),
		path:      path,
		interval:  interval,
		changeCh:  make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
		flushLock: &sync.Mutex{},
	}
	r.start()
	return r
}

// GetSystem returns a wrapped template system for a router.
func (r *JSONRegistry) GetSystem(key string) netflow.NetFlowTemplateSystem {
	r.lock.Lock()
	system, ok := r.systems[key]
	r.lock.Unlock()
	if ok {
		return system
	}

	wrapped := r.wrapped.GetSystem(key)
	system = &persistingTemplateSystem{
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
func (r *JSONRegistry) GetAll() map[string]netflow.FlowBaseTemplateSet {
	return r.wrapped.GetAll()
}

// Close stops background work and flushes pending data.
func (r *JSONRegistry) Close() {
	r.closeOnce.Do(func() {
		close(r.stopCh)
		<-r.doneCh
		r.flush()
		if closer, ok := r.wrapped.(RegistryCloser); ok {
			closer.Close()
		}
	})
}

func (r *JSONRegistry) start() {
	go func() {
		var timer *time.Timer
		defer close(r.doneCh)
		for {
			select {
			case <-r.changeCh:
				if r.interval <= 0 {
					r.flush()
					continue
				}
				if timer == nil {
					timer = time.NewTimer(r.interval)
				} else {
					if !timer.Stop() {
						<-timer.C
					}
					timer.Reset(r.interval)
				}
			case <-r.stopCh:
				if timer != nil {
					timer.Stop()
				}
				return
			case <-func() <-chan time.Time {
				if timer != nil {
					return timer.C
				}
				return nil
			}():
				timer = nil
				r.flush()
			}
		}
	}()
}

func (r *JSONRegistry) notifyChange() {
	if r.interval <= 0 {
		r.flush()
		return
	}
	select {
	case r.changeCh <- struct{}{}:
	default:
	}
}

func (r *JSONRegistry) flush() {
	r.flushLock.Lock()
	defer r.flushLock.Unlock()

	if r.path == "" {
		return
	}

	snapshot := r.wrapped.GetAll()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}

	_ = writeAtomic(r.path, data, 0o644)
}

type persistingTemplateSystem struct {
	key     string
	parent  *JSONRegistry
	wrapped netflow.NetFlowTemplateSystem
}

func (s *persistingTemplateSystem) AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) error {
	if err := s.wrapped.AddTemplate(version, obsDomainId, templateId, template); err != nil {
		return err
	}
	s.parent.notifyChange()
	return nil
}

func (s *persistingTemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	return s.wrapped.GetTemplate(version, obsDomainId, templateId)
}

func (s *persistingTemplateSystem) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	template, err := s.wrapped.RemoveTemplate(version, obsDomainId, templateId)
	if err == nil {
		s.parent.notifyChange()
	}
	return template, err
}

func (s *persistingTemplateSystem) GetTemplates() netflow.FlowBaseTemplateSet {
	return s.wrapped.GetTemplates()
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmpPath := path + "_tmp"
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

const defaultJSONFlushInterval = 10 * time.Second

// JSONRegistry persists templates to a JSON file with batched updates.
type JSONRegistry struct {
	lock      sync.RWMutex
	wrapped   Registry
	systems   map[string]netflow.NetFlowTemplateSystem
	path      string
	interval  time.Duration
	changeCh  chan struct{}
	flusherStop chan struct{}
	flusherDone chan struct{}
	flushLock sync.Mutex
	flushHook func()
	closeOnce sync.Once
	startOnce sync.Once
}

// NewJSONRegistry wraps a registry and persists templates to a JSON file.
// This handles writes only; loading stays separate so registry chains can preload first.
func NewJSONRegistry(path string, wrapped Registry) *JSONRegistry {
	if wrapped == nil {
		wrapped = NewInMemoryRegistry(nil)
	}
	r := &JSONRegistry{
		wrapped:  wrapped,
		systems:  make(map[string]netflow.NetFlowTemplateSystem),
		path:     path,
		interval: defaultJSONFlushInterval,
		changeCh: make(chan struct{}, 1),
	}
	return r
}

// PreloadJSONTemplates loads templates from a JSON file into the registry.
// This handles reads only; keep it separate from NewJSONRegistry to preserve chain load order.
func PreloadJSONTemplates(path string, registry Registry) error {
	if path == "" || registry == nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}

	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	type templateOp struct {
		routerKey   string
		version     uint16
		obsDomainId uint32
		templateId  uint16
		template    interface{}
	}
	var ops []templateOp

	for routerKey, templates := range raw {
		for keyStr, payload := range templates {
			version, obsDomainId, templateId, err := parseTemplateKey(keyStr)
			if err != nil {
				return fmt.Errorf("invalid template key %q: %w", keyStr, err)
			}
			template, err := decodeTemplatePayload(version, payload)
			if err != nil {
				return fmt.Errorf("invalid template payload for %q/%s: %w", routerKey, keyStr, err)
			}
			ops = append(ops, templateOp{
				routerKey:   routerKey,
				version:     version,
				obsDomainId: obsDomainId,
				templateId:  templateId,
				template:    template,
			})
		}
	}

	for _, op := range ops {
		system := registry.GetSystem(op.routerKey)
		if _, err := system.AddTemplate(op.version, op.obsDomainId, op.templateId, op.template); err != nil {
			return err
		}
	}

	return nil
}

// GetSystem returns a wrapped template system for a router.
func (r *JSONRegistry) GetSystem(key string) netflow.NetFlowTemplateSystem {
	r.lock.RLock()
	system, ok := r.systems[key]
	r.lock.RUnlock()
	if ok {
		return system
	}

	wrapped := r.wrapped.GetSystem(key)
	// Wrap the base system with JSON persistence hooks.
	system = &jsonPersistingTemplateSystem{
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

// Close stops background work and flushes pending data.
func (r *JSONRegistry) Close() {
	r.closeOnce.Do(func() {
		r.lock.Lock()
		if r.flusherStop == nil {
			r.lock.Unlock()
			r.flush()
			r.wrapped.Close()
			return
		}
		stop := r.flusherStop
		done := r.flusherDone
		r.flusherStop = nil
		r.flusherDone = nil
		r.lock.Unlock()

		close(stop)
		<-done
		r.flush()
		r.wrapped.Close()
	})
}

// Start begins background flush processing.
func (r *JSONRegistry) Start() {
	r.startOnce.Do(func() {
		r.wrapped.Start()
		r.StartFlush(r.interval)
	})
}

// StartFlush begins periodic flushing using the provided interval.
func (r *JSONRegistry) StartFlush(interval time.Duration) {
	if interval <= 0 {
		return
	}

	r.lock.Lock()
	r.interval = interval
	if r.flusherStop != nil {
		r.lock.Unlock()
		return
	}
	r.flusherStop = make(chan struct{})
	r.flusherDone = make(chan struct{})
	stopCh := r.flusherStop
	doneCh := r.flusherDone
	r.lock.Unlock()

	go func() {
		var timer *time.Timer
		defer close(doneCh)
		for {
			select {
			case <-r.changeCh:
				if timer == nil {
					timer = time.NewTimer(interval)
				} else {
					if !timer.Stop() {
						<-timer.C
					}
					timer.Reset(interval)
				}
			case <-stopCh:
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

// SetFlushInterval configures the flush interval for the next Start call only.
// It does not restart flushing if Start has already run.
func (r *JSONRegistry) SetFlushInterval(interval time.Duration) {
	r.lock.Lock()
	r.interval = interval
	r.lock.Unlock()
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
	if r.flushHook != nil {
		r.flushHook()
	}

	snapshot := r.wrapped.GetAll()
	filtered := make(map[string]map[string]interface{}, len(snapshot))
	for key, templates := range snapshot {
		if len(templates) == 0 {
			continue
		}
		encoded := make(map[string]interface{}, len(templates))
		for templateKey, template := range templates {
			version, obsDomainId, templateId := decodeTemplateKey(templateKey)
			encoded[formatTemplateKey(version, obsDomainId, templateId)] = template
		}
		filtered[key] = encoded
	}
	data, err := json.Marshal(filtered)
	if err != nil {
		return
	}

	_ = writeAtomic(r.path, data, 0o644)
}

// jsonPersistingTemplateSystem wraps a template system to trigger JSONRegistry flushes
// when templates change, while delegating storage and lookups to the wrapped system.
type jsonPersistingTemplateSystem struct {
	key     string
	parent  *JSONRegistry
	wrapped netflow.NetFlowTemplateSystem
}

func (s *jsonPersistingTemplateSystem) AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) (netflow.TemplateStatus, error) {
	update, err := s.wrapped.AddTemplate(version, obsDomainId, templateId, template)
	if err != nil {
		return update, err
	}
	if update != netflow.TemplateUnchanged {
		s.parent.notifyChange()
	}
	return update, nil
}

func (s *jsonPersistingTemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	return s.wrapped.GetTemplate(version, obsDomainId, templateId)
}

func (s *jsonPersistingTemplateSystem) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, bool, error) {
	template, removed, err := s.wrapped.RemoveTemplate(version, obsDomainId, templateId)
	if removed {
		s.parent.notifyChange()
	}
	return template, removed, err
}

func (s *jsonPersistingTemplateSystem) GetTemplates() netflow.FlowBaseTemplateSet {
	return s.wrapped.GetTemplates()
}

// RemoveSystem deletes a router entry if present.
func (r *JSONRegistry) RemoveSystem(key string) {
	r.lock.Lock()
	delete(r.systems, key)
	r.lock.Unlock()
	r.wrapped.RemoveSystem(key)
	r.notifyChange()
}

// writeAtomic writes data to a temp file, fsyncs it, then renames to avoid partial writes.
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
		if closeErr := tmpFile.Close(); closeErr != nil {
			return closeErr
		}
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			return closeErr
		}
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func decodeTemplateKey(key uint64) (uint16, uint32, uint16) {
	version := uint16(key >> 48)
	obsDomainId := uint32((key >> 16) & 0xFFFFFFFF)
	templateId := uint16(key & 0xFFFF)
	return version, obsDomainId, templateId
}

func formatTemplateKey(version uint16, obsDomainId uint32, templateId uint16) string {
	return fmt.Sprintf("%d/%d/%d", version, obsDomainId, templateId)
}

func parseTemplateKey(key string) (uint16, uint32, uint16, error) {
	if strings.Contains(key, "/") {
		parts := strings.Split(key, "/")
		if len(parts) != 3 {
			return 0, 0, 0, fmt.Errorf("expected version/obs-domain/template-id")
		}
		version, err := strconv.ParseUint(parts[0], 10, 16)
		if err != nil {
			return 0, 0, 0, err
		}
		obsDomainId, err := strconv.ParseUint(parts[1], 10, 32)
		if err != nil {
			return 0, 0, 0, err
		}
		templateId, err := strconv.ParseUint(parts[2], 10, 16)
		if err != nil {
			return 0, 0, 0, err
		}
		return uint16(version), uint32(obsDomainId), uint16(templateId), nil
	}
	return 0, 0, 0, fmt.Errorf("expected version/obs-domain/template-id")
}

func decodeTemplatePayload(version uint16, payload json.RawMessage) (interface{}, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, err
	}
	if _, ok := fields["scope-length"]; ok && version == 9 {
		var record netflow.NFv9OptionsTemplateRecord
		if err := json.Unmarshal(payload, &record); err != nil {
			return nil, err
		}
		return record, nil
	}
	if _, ok := fields["scope-field-count"]; ok && version == 10 {
		var record netflow.IPFIXOptionsTemplateRecord
		if err := json.Unmarshal(payload, &record); err != nil {
			return nil, err
		}
		return record, nil
	}
	var record netflow.TemplateRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, err
	}
	return record, nil
}

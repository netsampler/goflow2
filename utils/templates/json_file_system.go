// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

var (
	templateFileLocksMu sync.Mutex
	templateFileLocks   = map[string]*sync.Mutex{}
)

// JSONFileTemplateSystem wraps a template system and writes JSON snapshots to a shared file.
type JSONFileTemplateSystem struct {
	key     string
	wrapped netflow.NetFlowTemplateSystem
	path    string
	mu      *sync.Mutex
}

type jsonTemplateFile struct {
	Routers map[string][]jsonTemplateEntry `json:"routers"`
}

type jsonTemplateEntry struct {
	Version     uint16          `json:"version"`
	ObsDomainID uint32          `json:"obs_domain_id"`
	TemplateID  uint16          `json:"template_id"`
	Type        string          `json:"type"`
	Template    json.RawMessage `json:"template"`
}

// NewJSONFileTemplateSystem wraps a template system and writes JSON snapshots to a shared file.
func NewJSONFileTemplateSystem(key string, wrapped netflow.NetFlowTemplateSystem, path string) netflow.NetFlowTemplateSystem {
	system := &JSONFileTemplateSystem{
		key:     key,
		wrapped: wrapped,
		path:    path,
		mu:      templateFileLock(path),
	}
	system.load()
	return system
}

// AddTemplate stores a template and optionally writes the snapshot as JSON.
func (s *JSONFileTemplateSystem) AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) error {
	err := s.wrapped.AddTemplate(version, obsDomainId, templateId, template)
	if err == nil {
		s.writeSnapshot()
	}
	return err
}

// GetTemplate retrieves a template from the wrapped system.
func (s *JSONFileTemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	return s.wrapped.GetTemplate(version, obsDomainId, templateId)
}

// RemoveTemplate removes a template and optionally writes the snapshot as JSON.
func (s *JSONFileTemplateSystem) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	template, err := s.wrapped.RemoveTemplate(version, obsDomainId, templateId)
	if err == nil {
		s.writeSnapshot()
	}
	return template, err
}

// GetTemplates returns all templates from the wrapped system.
func (s *JSONFileTemplateSystem) GetTemplates() netflow.FlowBaseTemplateSet {
	return s.wrapped.GetTemplates()
}

func (s *JSONFileTemplateSystem) writeSnapshot() {
	if s.path == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file := jsonTemplateFile{Routers: map[string][]jsonTemplateEntry{}}
	if payload, err := os.ReadFile(s.path); err == nil {
		if len(payload) > 0 {
			if err := json.Unmarshal(payload, &file); err != nil {
				slog.Error("error decoding template JSON file", slog.String("error", err.Error()))
				file = jsonTemplateFile{Routers: map[string][]jsonTemplateEntry{}}
			}
		}
	} else if !os.IsNotExist(err) {
		slog.Error("error reading template JSON file", slog.String("error", err.Error()))
	}

	entries := make([]jsonTemplateEntry, 0)
	for key, template := range s.wrapped.GetTemplates() {
		version, obsDomainID, templateID := splitTemplateKey(key)
		templateType, templateBody := encodeTemplate(template)
		if templateType == "" {
			continue
		}
		entries = append(entries, jsonTemplateEntry{
			Version:     version,
			ObsDomainID: obsDomainID,
			TemplateID:  templateID,
			Type:        templateType,
			Template:    templateBody,
		})
	}

	if file.Routers == nil {
		file.Routers = map[string][]jsonTemplateEntry{}
	}
	file.Routers[s.key] = entries

	payload, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		slog.Error("error encoding template JSON file", slog.String("error", err.Error()))
		return
	}

	if err := os.WriteFile(s.path, payload, 0o644); err != nil {
		slog.Error("error writing template JSON file", slog.String("error", err.Error()))
	}
}

func (s *JSONFileTemplateSystem) load() {
	if s.path == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		slog.Error("error reading template JSON file", slog.String("error", err.Error()))
		return
	}
	if len(payload) == 0 {
		return
	}

	var file jsonTemplateFile
	if err := json.Unmarshal(payload, &file); err != nil {
		slog.Error("error decoding template JSON file", slog.String("error", err.Error()))
		return
	}

	for _, entry := range file.Routers[s.key] {
		template, err := decodeTemplate(entry)
		if err != nil {
			slog.Error("error decoding template entry", slog.String("error", err.Error()))
			continue
		}
		if err := s.wrapped.AddTemplate(entry.Version, entry.ObsDomainID, entry.TemplateID, template); err != nil {
			slog.Error("error loading template entry", slog.String("error", err.Error()))
		}
	}
}

func templateFileLock(path string) *sync.Mutex {
	if path == "" {
		return &sync.Mutex{}
	}
	templateFileLocksMu.Lock()
	defer templateFileLocksMu.Unlock()
	if lock, ok := templateFileLocks[path]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	templateFileLocks[path] = lock
	return lock
}

func splitTemplateKey(key uint64) (uint16, uint32, uint16) {
	version := uint16(key >> 48)
	obsDomainID := uint32((key >> 16) & 0xffffffff)
	templateID := uint16(key & 0xffff)
	return version, obsDomainID, templateID
}

func encodeTemplate(template interface{}) (string, json.RawMessage) {
	switch typed := template.(type) {
	case netflow.TemplateRecord:
		body, _ := json.Marshal(typed)
		return "template", body
	case netflow.IPFIXOptionsTemplateRecord:
		body, _ := json.Marshal(typed)
		return "ipfix_options_template", body
	case netflow.NFv9OptionsTemplateRecord:
		body, _ := json.Marshal(typed)
		return "nfv9_options_template", body
	default:
		return "", nil
	}
}

func decodeTemplate(entry jsonTemplateEntry) (interface{}, error) {
	switch entry.Type {
	case "template":
		var record netflow.TemplateRecord
		if err := json.Unmarshal(entry.Template, &record); err != nil {
			return nil, err
		}
		return record, nil
	case "ipfix_options_template":
		var record netflow.IPFIXOptionsTemplateRecord
		if err := json.Unmarshal(entry.Template, &record); err != nil {
			return nil, err
		}
		return record, nil
	case "nfv9_options_template":
		var record netflow.NFv9OptionsTemplateRecord
		if err := json.Unmarshal(entry.Template, &record); err != nil {
			return nil, err
		}
		return record, nil
	default:
		return nil, fmt.Errorf("unknown template type %q", entry.Type)
	}
}

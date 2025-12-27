// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strconv"
	"sync"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

var (
	templateWriterLocksMu sync.Mutex
	templateWriterLocks   = map[uintptr]*sync.Mutex{}
)

// JSONFileTemplateSystem wraps a template system and writes JSON snapshots to a shared file.
type JSONFileTemplateSystem struct {
	key     string
	wrapped netflow.NetFlowTemplateSystem
	writer  io.ReadWriteSeeker
	mu      *sync.Mutex
}

type jsonTemplateFile struct {
	Routers map[string]jsonRouterTemplates `json:"routers"`
}

type jsonRouterTemplates struct {
	Versions map[string]jsonVersionTemplates `json:"versions"`
}

type jsonVersionTemplates struct {
	ObsDomains map[string]jsonObsDomainTemplates `json:"obs_domains"`
}

type jsonObsDomainTemplates struct {
	Templates map[string]jsonTemplateEntry `json:"templates"`
}

type jsonTemplateEntry struct {
	Type     string          `json:"type"`
	Template json.RawMessage `json:"template"`
}

// NewJSONFileTemplateSystem wraps a template system and writes JSON snapshots to a shared file.
func NewJSONFileTemplateSystem(key string, wrapped netflow.NetFlowTemplateSystem, writer io.ReadWriteSeeker) netflow.NetFlowTemplateSystem {
	system := &JSONFileTemplateSystem{
		key:     key,
		wrapped: wrapped,
		writer:  writer,
		mu:      templateWriterLock(writer),
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
	if s.writer == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file := jsonTemplateFile{Routers: map[string]jsonRouterTemplates{}}
	payload, err := readAllAtStart(s.writer)
	if err == nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, &file); err != nil {
			slog.Error("error decoding template JSON file", slog.String("error", err.Error()))
			file = jsonTemplateFile{Routers: map[string]jsonRouterTemplates{}}
		}
	} else if err != nil && err != io.EOF {
		slog.Error("error reading template JSON file", slog.String("error", err.Error()))
	}

	if file.Routers == nil {
		file.Routers = map[string]jsonRouterTemplates{}
	}
	routerTemplates := jsonRouterTemplates{Versions: map[string]jsonVersionTemplates{}}

	for key, template := range s.wrapped.GetTemplates() {
		version, obsDomainID, templateID := splitTemplateKey(key)
		templateType, templateBody := encodeTemplate(template)
		if templateType == "" {
			continue
		}
		versionKey := strconv.FormatUint(uint64(version), 10)
		obsDomainKey := strconv.FormatUint(uint64(obsDomainID), 10)
		versionTemplates := routerTemplates.Versions[versionKey]
		if versionTemplates.ObsDomains == nil {
			versionTemplates.ObsDomains = map[string]jsonObsDomainTemplates{}
		}
		obsTemplates := versionTemplates.ObsDomains[obsDomainKey]
		if obsTemplates.Templates == nil {
			obsTemplates.Templates = map[string]jsonTemplateEntry{}
		}
		obsTemplates.Templates[strconv.FormatUint(uint64(templateID), 10)] = jsonTemplateEntry{
			Type:     templateType,
			Template: templateBody,
		}
		versionTemplates.ObsDomains[obsDomainKey] = obsTemplates
		routerTemplates.Versions[versionKey] = versionTemplates
	}

	file.Routers[s.key] = routerTemplates

	payload, err = json.MarshalIndent(file, "", "  ")
	if err != nil {
		slog.Error("error encoding template JSON file", slog.String("error", err.Error()))
		return
	}
	if err := writeAllAtStart(s.writer, payload); err != nil {
		slog.Error("error writing template JSON file", slog.String("error", err.Error()))
	}
}

func (s *JSONFileTemplateSystem) load() {
	if s.writer == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := readAllAtStart(s.writer)
	if err != nil && err != io.EOF {
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

	routerTemplates, ok := file.Routers[s.key]
	if !ok {
		return
	}

	for versionKey, versionTemplates := range routerTemplates.Versions {
		version, err := strconv.ParseUint(versionKey, 10, 16)
		if err != nil {
			slog.Error("error parsing template version", slog.String("error", err.Error()))
			continue
		}
		for obsDomainKey, obsTemplates := range versionTemplates.ObsDomains {
			obsDomainID, err := strconv.ParseUint(obsDomainKey, 10, 32)
			if err != nil {
				slog.Error("error parsing template observation domain", slog.String("error", err.Error()))
				continue
			}
			for templateKey, entry := range obsTemplates.Templates {
				templateID, err := strconv.ParseUint(templateKey, 10, 16)
				if err != nil {
					slog.Error("error parsing template id", slog.String("error", err.Error()))
					continue
				}
				template, err := decodeTemplate(entry)
				if err != nil {
					slog.Error("error decoding template entry", slog.String("error", err.Error()))
					continue
				}
				if err := s.wrapped.AddTemplate(uint16(version), uint32(obsDomainID), uint16(templateID), template); err != nil {
					slog.Error("error loading template entry", slog.String("error", err.Error()))
				}
			}
		}
	}
}

func templateWriterLock(writer io.ReadWriteSeeker) *sync.Mutex {
	if writer == nil {
		return &sync.Mutex{}
	}
	value := reflect.ValueOf(writer)
	if value.Kind() != reflect.Ptr {
		return &sync.Mutex{}
	}
	key := value.Pointer()
	templateWriterLocksMu.Lock()
	defer templateWriterLocksMu.Unlock()
	if lock, ok := templateWriterLocks[key]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	templateWriterLocks[key] = lock
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

func readAllAtStart(reader io.ReadSeeker) ([]byte, error) {
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(reader)
}

func writeAllAtStart(writer io.ReadWriteSeeker, payload []byte) error {
	if _, err := writer.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if resetWriter, ok := writer.(interface {
		Truncate(int64) error
	}); ok {
		if err := resetWriter.Truncate(0); err != nil {
			return err
		}
	}
	_, err := writer.Write(payload)
	if err != nil {
		return err
	}
	if syncer, ok := writer.(interface {
		Sync() error
	}); ok {
		return syncer.Sync()
	}
	return nil
}

// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
)

// PersistenceHooks returns template hooks that only notify persistence on changes.
func PersistenceHooks(notifyChange func()) TemplateHooks {
	return TemplateHooks{
		OnAdd: func(router string, version uint16, obsDomainId uint32, templateId uint16, template interface{}, _ bool) {
			if notifyChange != nil {
				notifyChange()
			}
		},
		OnRemove: func(router string, version uint16, obsDomainId uint32, templateId uint16, _ interface{}) {
			if notifyChange != nil {
				notifyChange()
			}
		},
	}
}

// MarshalJSONSnapshot marshals the current store contents directly from a snapshot.
func MarshalJSONSnapshot(store netflow.ManagedTemplateStore) ([]byte, error) {
	if store == nil {
		return json.Marshal(map[string]map[string]interface{}{})
	}
	snapshot := store.GetAll()
	filtered := make(map[string]map[string]interface{}, len(snapshot))
	for router, templatesByKey := range snapshot {
		if len(templatesByKey) == 0 {
			continue
		}
		encoded := make(map[string]interface{}, len(templatesByKey))
		for templateKey, template := range templatesByKey {
			version, obsDomainId, templateId := decodeTemplateKey(templateKey)
			encoded[formatTemplateKey(version, obsDomainId, templateId)] = template
		}
		filtered[router] = encoded
	}
	return json.Marshal(filtered)
}

// LoadJSON populates the store from a JSON buffer.
func LoadJSON(store netflow.TemplateStore, buf []byte) error {
	if store == nil || len(buf) == 0 {
		return nil
	}
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(buf, &raw); err != nil {
		return fmt.Errorf("decode templates: %w", err)
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
		ctx := netflow.FlowContext{RouterKey: op.routerKey}
		if _, err := store.AddTemplate(ctx, op.version, op.obsDomainId, op.templateId, op.template); err != nil {
			return fmt.Errorf("preload templates: add %s: %w", op.routerKey, err)
		}
	}
	return nil
}

// PreloadJSONTemplates loads templates from a JSON file into the store.
func PreloadJSONTemplates(path string, store netflow.TemplateStore) error {
	if path == "" || store == nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("preload templates %s: read: %w", path, err)
	}
	if err := LoadJSON(store, data); err != nil {
		return fmt.Errorf("preload templates %s: %w", path, err)
	}
	return nil
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
			return 0, 0, 0, fmt.Errorf("parse template version: %w", err)
		}
		obsDomainId, err := strconv.ParseUint(parts[1], 10, 32)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("parse template obs-domain: %w", err)
		}
		templateId, err := strconv.ParseUint(parts[2], 10, 16)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("parse template id: %w", err)
		}
		return uint16(version), uint32(obsDomainId), uint16(templateId), nil
	}
	return 0, 0, 0, fmt.Errorf("expected version/obs-domain/template-id")
}

func decodeTemplatePayload(version uint16, payload json.RawMessage) (interface{}, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, fmt.Errorf("decode template payload: %w", err)
	}
	if _, ok := fields["scope-length"]; ok && version == 9 {
		var record netflow.NFv9OptionsTemplateRecord
		if err := json.Unmarshal(payload, &record); err != nil {
			return nil, fmt.Errorf("decode NFv9 options template: %w", err)
		}
		return record, nil
	}
	if _, ok := fields["scope-field-count"]; ok && version == 10 {
		var record netflow.IPFIXOptionsTemplateRecord
		if err := json.Unmarshal(payload, &record); err != nil {
			return nil, fmt.Errorf("decode IPFIX options template: %w", err)
		}
		return record, nil
	}
	var record netflow.TemplateRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, fmt.Errorf("decode template record: %w", err)
	}
	return record, nil
}

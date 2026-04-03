package samplingrate

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
)

// PersistenceHooks returns sampling-rate hooks that only notify persistence on changes.
func PersistenceHooks(notifyChange func()) Hooks {
	return Hooks{
		OnSet: func(router string, version uint16, obsDomainId uint32, rate uint32, _ bool) {
			if notifyChange != nil {
				notifyChange()
			}
		},
		OnRemove: func(router string, version uint16, obsDomainId uint32, rate uint32) {
			if notifyChange != nil {
				notifyChange()
			}
		},
	}
}

// MarshalJSONSnapshot marshals the current store contents directly from a snapshot.
func MarshalJSONSnapshot(store Store) ([]byte, error) {
	if store == nil {
		return json.Marshal(map[string]map[string]uint32{})
	}
	snapshot := store.GetAll()
	filtered := make(map[string]map[string]uint32, len(snapshot))
	for router, entries := range snapshot {
		if len(entries) == 0 {
			continue
		}
		encoded := make(map[string]uint32, len(entries))
		for key, rate := range entries {
			version, obsDomainId := decodeSamplingKey(key)
			encoded[formatSamplingKey(version, obsDomainId)] = rate
		}
		filtered[router] = encoded
	}
	return json.Marshal(filtered)
}

// LoadJSON populates the store from a JSON buffer.
func LoadJSON(store Store, buf []byte) error {
	if store == nil || len(buf) == 0 {
		return nil
	}
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(buf, &raw); err != nil {
		return fmt.Errorf("decode sampling rates: %w", err)
	}
	for routerKey, entries := range raw {
		for keyStr, payload := range entries {
			var rate uint32
			if err := json.Unmarshal(payload, &rate); err != nil {
				return fmt.Errorf("decode sampling rate %s %s: %w", routerKey, keyStr, err)
			}
			version, obsDomainId, err := parseSamplingKey(keyStr)
			if err != nil {
				return fmt.Errorf("invalid sampling rate key %q: %w", keyStr, err)
			}
			ctx := netflow.FlowContext{RouterKey: routerKey}
			if err := store.Set(ctx, version, obsDomainId, rate); err != nil {
				return fmt.Errorf("preload sampling rate %s %s: %w", routerKey, keyStr, err)
			}
		}
	}
	return nil
}

// PreloadJSONSamplingRates loads sampling rates from JSON into the store.
func PreloadJSONSamplingRates(path string, store Store) error {
	if path == "" || store == nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("preload sampling rates %s: read: %w", path, err)
	}
	if err := LoadJSON(store, data); err != nil {
		return fmt.Errorf("preload sampling rates %s: %w", path, err)
	}
	return nil
}

func parseSamplingKey(key string) (uint16, uint32, error) {
	parts := strings.Split(key, "/")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected version/obs-domain")
	}
	version, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("parse sampling version: %w", err)
	}
	obsDomainId, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("parse sampling obs-domain: %w", err)
	}
	return uint16(version), uint32(obsDomainId), nil
}

func formatSamplingKey(version uint16, obsDomainId uint32) string {
	return fmt.Sprintf("%d/%d", version, obsDomainId)
}

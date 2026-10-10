// Package flowkey defines the packed keys used in flow-store snapshots.
package flowkey

import "fmt"

// Template packs version, observation domain, and template ID into a snapshot key.
func Template(version uint16, obsDomainID uint32, templateID uint16) uint64 {
	return uint64(version)<<48 | uint64(obsDomainID)<<16 | uint64(templateID)
}

// SplitTemplate unpacks a template snapshot key.
func SplitTemplate(key uint64) (uint16, uint32, uint16) {
	return uint16(key >> 48), uint32(key >> 16), uint16(key)
}

// FormatTemplate renders a snapshot key as version/obs-domain/template-id.
func FormatTemplate(key uint64) string {
	version, obsDomainID, templateID := SplitTemplate(key)
	return fmt.Sprintf("%d/%d/%d", version, obsDomainID, templateID)
}

// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

// TemplateSystemGenerator creates a template system for a source key.
type TemplateSystemGenerator func(key string) netflow.NetFlowTemplateSystem

// DefaultTemplateGenerator creates a basic in-memory template system.
func DefaultTemplateGenerator(key string) netflow.NetFlowTemplateSystem {
	return netflow.CreateTemplateSystem()
}

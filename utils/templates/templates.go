// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"io"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

// TemplateSystemGenerator creates a template system for a source key.
type TemplateSystemGenerator func(key string) netflow.NetFlowTemplateSystem

// DefaultTemplateGenerator creates a basic in-memory template system.
func DefaultTemplateGenerator(key string) netflow.NetFlowTemplateSystem {
	return netflow.CreateTemplateSystem()
}

// NewJSONFileTemplateSystemGenerator wraps a generator with JSON file persistence.
func NewJSONFileTemplateSystemGenerator(writer io.ReadWriteSeeker, wrapped TemplateSystemGenerator) TemplateSystemGenerator {
	return func(key string) netflow.NetFlowTemplateSystem {
		var base netflow.NetFlowTemplateSystem
		if wrapped != nil {
			base = wrapped(key)
		} else {
			base = netflow.CreateTemplateSystem()
		}
		if writer == nil {
			return base
		}
		return NewJSONFileTemplateSystem(key, base, writer)
	}
}

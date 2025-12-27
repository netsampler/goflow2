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

// JSONFileTemplateGenerator builds template systems backed by a shared JSON file.
type JSONFileTemplateGenerator struct {
	writer  AtomicWriter
	wrapped TemplateSystemGenerator
}

// NewJSONFileTemplateSystemGenerator wraps a generator with JSON file persistence.
func NewJSONFileTemplateSystemGenerator(writer AtomicWriter, wrapped TemplateSystemGenerator) *JSONFileTemplateGenerator {
	return &JSONFileTemplateGenerator{
		writer:  writer,
		wrapped: wrapped,
	}
}

// Generator returns a TemplateSystemGenerator for NetFlow sources.
func (g *JSONFileTemplateGenerator) Generator() TemplateSystemGenerator {
	return func(key string) netflow.NetFlowTemplateSystem {
		var base netflow.NetFlowTemplateSystem
		if g.wrapped != nil {
			base = g.wrapped(key)
		} else {
			base = netflow.CreateTemplateSystem()
		}
		if g.writer == nil {
			return base
		}
		return NewJSONFileTemplateSystem(key, base, g.writer)
	}
}

// Close releases the underlying writer if present.
func (g *JSONFileTemplateGenerator) Close() error {
	if g.writer == nil {
		return nil
	}
	return g.writer.Close()
}

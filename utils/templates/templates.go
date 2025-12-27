// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

// ManagedTemplateSystem adds lifecycle management to a NetFlow template system.
type ManagedTemplateSystem interface {
	netflow.NetFlowTemplateSystem
	Close() error
}

// TemplateSystemGenerator creates a template system for a source key.
type TemplateSystemGenerator func(key string) ManagedTemplateSystem

// DefaultTemplateGenerator creates a basic in-memory template system.
func DefaultTemplateGenerator(key string) ManagedTemplateSystem {
	return netflow.CreateTemplateSystem().(ManagedTemplateSystem)
}

// BasicTemplateSystemGenerator creates a basic in-memory template system.
func BasicTemplateSystemGenerator(key string) ManagedTemplateSystem {
	return DefaultTemplateGenerator(key)
}

// JSONFileTemplateGenerator builds template systems backed by a shared JSON file.
type JSONFileTemplateGenerator struct {
	writer   AtomicWriter
	wrapped  TemplateSystemGenerator
	interval time.Duration
}

// NewJSONFileTemplateSystemGenerator wraps a generator with JSON file persistence.
func NewJSONFileTemplateSystemGenerator(writer AtomicWriter, wrapped TemplateSystemGenerator, interval time.Duration) *JSONFileTemplateGenerator {
	return &JSONFileTemplateGenerator{
		writer:   writer,
		wrapped:  wrapped,
		interval: interval,
	}
}

// Generator returns a TemplateSystemGenerator for NetFlow sources.
func (g *JSONFileTemplateGenerator) Generator() TemplateSystemGenerator {
	return func(key string) ManagedTemplateSystem {
		var base ManagedTemplateSystem
		if g.wrapped != nil {
			base = g.wrapped(key)
		} else {
			base = netflow.CreateTemplateSystem().(ManagedTemplateSystem)
		}
		if g.writer == nil {
			return base
		}
		return NewJSONFileTemplateSystem(key, base, g.writer, g.interval)
	}
}

// Close releases the underlying writer if present.
func (g *JSONFileTemplateGenerator) Close() error {
	if g.writer == nil {
		return nil
	}
	return g.writer.Close()
}

// TemplateSystemRemover deletes a template system for a source key.
type TemplateSystemRemover interface {
	Remove(key string)
}

// Remove deletes a template system from a remover if provided.
func Remove(remover TemplateSystemRemover, key string) {
	if remover == nil {
		return
	}
	remover.Remove(key)
}

package decode

import (
	"fmt"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/utils/store/persistence"
	"github.com/netsampler/goflow2/v3/utils/store/samplingrate"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
)

// Decoder identifies and decodes source payloads into the next runtime event shape.
type Decoder interface {
	Decode(evt *event.Event) ([]*event.Event, error)
	Errors() <-chan error
	Close()
}

// Options configures the built-in decoder.
type Options struct {
	Catalog             map[string]config.IPFIXFieldDefinition
	EmbedTemplateFields bool
	CachePath           string
	CacheInterval       time.Duration
}

// New returns the built-in decoder used by the current runtime.
func New() Decoder {
	return NewWithCatalog(nil)
}

// NewWithCatalog returns a decoder that uses the shared templated field catalog
// for IPFIX and NetFlow v9 data record expansion.
func NewWithCatalog(catalog map[string]config.IPFIXFieldDefinition) Decoder {
	decoder, err := NewWithOptions(Options{Catalog: catalog})
	if err != nil {
		panic(err)
	}
	return decoder
}

// NewWithOptions returns a decoder with optional template and sampling-rate cache persistence.
func NewWithOptions(opts Options) (Decoder, error) {
	var (
		manager  *persistence.Manager
		store    netflow.ManagedTemplateStore
		sampling samplingrate.Store
		err      error
	)
	if opts.CachePath != "" {
		manager = persistence.New(persistence.Config{
			Path:     opts.CachePath,
			Interval: opts.CacheInterval,
		})
		sampling, err = manager.NewSamplingRateStore()
		if err != nil {
			manager.Close()
			return nil, fmt.Errorf("init sampling cache: %w", err)
		}
		store, err = manager.NewTemplateStore()
		if err != nil {
			manager.Close()
			return nil, fmt.Errorf("init template cache: %w", err)
		}
	} else {
		store = templates.NewTemplateFlowStore()
		sampling = samplingrate.NewSamplingRateFlowStore()
	}
	store.Start()
	sampling.Start()
	if manager != nil {
		manager.Start()
	}
	return &builtIn{
		templates:           store,
		sampling:            sampling,
		catalog:             newDecodeCatalog(opts.Catalog),
		embedTemplateFields: opts.EmbedTemplateFields,
		persistence:         manager,
	}, nil
}

type builtIn struct {
	templates           netflow.ManagedTemplateStore
	sampling            samplingrate.Store
	catalog             decodeCatalog
	embedTemplateFields bool
	persistence         *persistence.Manager
}

// Close stops decoder-owned stores and their background sweepers.
func (d *builtIn) Close() {
	if d == nil {
		return
	}
	if d.templates != nil {
		d.templates.Close()
	}
	if d.sampling != nil {
		d.sampling.Close()
	}
	if d.persistence != nil {
		d.persistence.Close()
	}
}

// Errors exposes asynchronous decoder cache persistence errors.
func (d *builtIn) Errors() <-chan error {
	if d == nil || d.persistence == nil {
		return nil
	}
	return d.persistence.Errors()
}

// Decode handles protocol identification for raw flow payloads and passes through other event types.
func (d *builtIn) Decode(evt *event.Event) ([]*event.Event, error) {
	if evt != nil && evt.Kind == "control" {
		return []*event.Event{evt}, nil
	}
	switch evt.Source.Type {
	case "flow":
		return d.decodeFlow(evt)
	case "bytes":
		return decodeBytes(evt)
	default:
		return []*event.Event{evt}, nil
	}
}

// decodeBytes marks raw packet inputs as bytes payloads and leaves the actual
// tuple extraction to the processor stage.
func decodeBytes(evt *event.Event) ([]*event.Event, error) {
	payload, ok := evt.Payload.([]byte)
	if !ok || len(payload) == 0 {
		return nil, fmt.Errorf("decode bytes: missing payload bytes")
	}
	if evt.Fields == nil {
		evt.Fields = make(map[string]any, 1)
	}
	return []*event.Event{evt}, nil
}

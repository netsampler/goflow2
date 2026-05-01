package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/aggregate"
	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/decode"
	"github.com/netsampler/goflow2/v3/internal/reflow/encode"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/internal/reflow/processor"
	"github.com/netsampler/goflow2/v3/internal/reflow/sink"
	"github.com/netsampler/goflow2/v3/internal/reflow/source"
	"github.com/netsampler/goflow2/v3/pkg/goflow2/logging"
)

type App struct {
	logger           *slog.Logger
	sources          []source.Source
	decoder          decode.Decoder
	processor        processor.Processor
	processorWorkers int
	aggregatorCfgs   []config.AggregatorConfig
	encoderCfg       config.EncoderConfig
	encoderWorkers   int
	sink             sink.Sink
}

// New wires the current ReFlow runtime from source to sink.
func New(cfg *config.Config) (*App, error) {
	logger, err := logging.NewLogger(cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		return nil, fmt.Errorf("init logger: %w", err)
	}
	slog.SetDefault(logger)

	sources := make([]source.Source, 0, len(cfg.Sources))
	for i, srcCfg := range cfg.Sources {
		src, err := source.New(srcCfg)
		if err != nil {
			return nil, fmt.Errorf("init source %d: %w", i, err)
		}
		sources = append(sources, src)
	}
	proc, err := processor.New(cfg.Processor)
	if err != nil {
		return nil, fmt.Errorf("init processor: %w", err)
	}
	out, err := sink.New(cfg.Sink)
	if err != nil {
		return nil, fmt.Errorf("init sink: %w", err)
	}

	encoderCfg := cfg.Encoder
	if cfg.Sink.AgentIP != "" && encoderCfg.Type == "sflow" {
		// The sink-level agent IP acts as a final override for exported sFlow packets.
		encoderCfg.SFlow.AgentIP = cfg.Sink.AgentIP
	}
	encoderWorkers := cfg.Encoder.Workers
	// Ordered exporters own protocol sequence numbers and template refresh state.
	// The effective config is:
	//
	//   encoder:
	//     type: sflow # also ipfix, netflowv9, or netflowv5
	//     workers: 1
	//
	if requiresOrderedExporter(encoderCfg.Type) && encoderWorkers > 1 {
		logger.Warn(
			"forcing encoder workers to 1 for ordered exporter",
			slog.String("encoder_type", encoderCfg.Type),
			slog.Int("requested_workers", encoderWorkers),
		)
		encoderWorkers = 1
	}

	return &App{
		logger:           logger,
		sources:          sources,
		decoder:          decode.New(),
		processor:        proc,
		processorWorkers: cfg.Processor.Workers,
		aggregatorCfgs:   cfg.Aggregators,
		encoderCfg:       encoderCfg,
		encoderWorkers:   encoderWorkers,
		sink:             out,
	}, nil
}

// Run owns the source, processor, aggregation, encoding, and sink goroutines.
func (a *App) Run(ctx context.Context) error {
	a.logger.Info("starting ReFlow")
	defer a.sink.Close()
	defer a.decoder.Close()

	sourceCtx, stopSource := context.WithCancel(ctx)
	defer stopSource()

	decodeJobs := make(chan *event.Event, a.processorWorkers*2)
	processJobs := make(chan *event.Event, a.processorWorkers*2)
	encodeJobs := make(chan *event.Event, a.encoderWorkers*2)

	var decodeWG sync.WaitGroup
	decodeWG.Add(1)
	go func() {
		defer decodeWG.Done()
		// Decode is centralized so protocol identification and shared state stay
		// single-writer even when later stages fan out with workers.
		for evt := range decodeJobs {
			events, err := a.decoder.Decode(evt)
			if err != nil {
				a.logger.Error("decode error", slog.String("error", err.Error()))
				continue
			}
			for _, item := range events {
				processJobs <- item
			}
		}
	}()

	var aggregateWG sync.WaitGroup
	aggregateWorkers := make([]aggregateWorker, 0, len(a.aggregatorCfgs))
	for _, aggCfg := range a.aggregatorCfgs {
		agg, err := aggregate.New(aggCfg)
		if err != nil {
			return fmt.Errorf("init aggregator %q: %w", aggCfg.Stream, err)
		}
		worker := aggregateWorker{
			cfg:  aggCfg,
			agg:  agg,
			jobs: make(chan *event.Event, a.processorWorkers*2),
		}
		aggregateWorkers = append(aggregateWorkers, worker)
		aggregateWG.Add(1)
		go func(worker aggregateWorker) {
			defer aggregateWG.Done()
			var ticker *time.Ticker
			if interval := worker.agg.Interval(); interval > 0 {
				ticker = time.NewTicker(interval)
				defer ticker.Stop()
			}
			sendToEncoder := func(events []*event.Event) {
				for _, evt := range events {
					encodeJobs <- evt
				}
			}
			// flush is shared by timer-driven and shutdown-driven paths so
			// aggregate close semantics stay consistent.
			flush := func(closeStore bool) {
				var (
					events []*event.Event
					err    error
				)
				if closeStore {
					events, err = worker.agg.Close()
				} else {
					events, err = worker.agg.Flush()
				}
				if err != nil {
					a.logger.Error("aggregate flush error", slog.String("stream", worker.cfg.Stream), slog.String("error", err.Error()))
					return
				}
				// Aggregator Flush/Close returns bucket events that are ready
				// to leave the aggregation stage, so pass them to the encoder.
				sendToEncoder(events)
			}
			for {
				select {
				case <-tickerChannel(ticker):
					flush(false)
				case evt, ok := <-worker.jobs:
					if !ok {
						flush(true)
						return
					}
					events, err := worker.agg.Process(evt)
					if err != nil {
						a.logger.Error("aggregate error", slog.String("stream", worker.cfg.Stream), slog.String("error", err.Error()))
						continue
					}
					sendToEncoder(events)
				}
			}
		}(worker)
	}
	hasAggregators := len(aggregateWorkers) > 0
	var aggregateRouteJobs chan *event.Event
	if hasAggregators {
		aggregateRouteJobs = make(chan *event.Event, a.processorWorkers*2)
	}

	var processWG sync.WaitGroup
	for range a.processorWorkers {
		processWG.Add(1)
		go func() {
			defer processWG.Done()
			for evt := range processJobs {
				events, err := a.processor.Process(evt)
				if err != nil {
					a.logger.Error("process error", slog.String("error", err.Error()))
					continue
				}
				for _, item := range events {
					if hasAggregators {
						aggregateRouteJobs <- item
					} else {
						encodeJobs <- item
					}
				}
			}
		}()
	}

	var aggregateRouteWG sync.WaitGroup
	if hasAggregators {
		aggregateRouteWG.Add(1)
		go func() {
			defer aggregateRouteWG.Done()
			for evt := range aggregateRouteJobs {
				if evt == nil {
					continue
				}
				if evt.Kind == "control" {
					encodeJobs <- evt
					continue
				}
				matched := false
				for _, worker := range aggregateWorkers {
					if !aggregatorMatches(worker.cfg, evt) {
						continue
					}
					matched = true
					// One event may intentionally feed multiple aggregators.
					worker.jobs <- evt
				}
				if !matched {
					encodeJobs <- evt
				}
			}
			for _, worker := range aggregateWorkers {
				close(worker.jobs)
			}
		}()
	} else {
		for _, worker := range aggregateWorkers {
			close(worker.jobs)
		}
	}

	var encodeWG sync.WaitGroup
	encoders := make([]encode.Encoder, 0, a.encoderWorkers)
	for i := range a.encoderWorkers {
		enc, err := encode.New(a.encoderCfg)
		if err != nil {
			return fmt.Errorf("init encoder %d: %w", i, err)
		}
		encoders = append(encoders, enc)
	}
	for _, enc := range encoders {
		encodeWG.Add(1)
		go func(enc encode.Encoder) {
			defer encodeWG.Done()
			var ticker *time.Ticker
			if interval := encoderTickInterval(a.encoderCfg); interval > 0 {
				ticker = time.NewTicker(interval)
				defer ticker.Stop()
			}
			flush := func() bool {
				payloads, err := enc.Flush()
				if err != nil {
					a.logger.Error("encode flush error", slog.String("error", err.Error()))
					return true
				}
				for _, payload := range payloads {
					if err := a.sink.Send(payload); err != nil {
						a.logger.Error("sink write error", slog.String("error", err.Error()))
					}
				}
				return true
			}
			for {
				select {
				case <-tickerChannel(ticker):
					if !flush() {
						return
					}
				case evt, ok := <-encodeJobs:
					if !ok {
						flush()
						return
					}
					payloads, err := enc.Encode(evt)
					if err != nil {
						a.logger.Error("encode error", slog.String("error", err.Error()))
						continue
					}
					for _, payload := range payloads {
						if err := a.sink.Send(payload); err != nil {
							a.logger.Error("sink write error", slog.String("error", err.Error()))
						}
					}
				}
			}
		}(enc)
	}

	for _, worker := range aggregateWorkers {
		initEvents, err := worker.agg.InitEvents()
		if err != nil {
			return fmt.Errorf("init aggregator events for %q: %w", worker.cfg.Stream, err)
		}
		for _, evt := range initEvents {
			encodeJobs <- evt
		}
	}
	for i, src := range a.sources {
		sourceInitEvents, err := src.InitEvents()
		if err != nil {
			return fmt.Errorf("init source %d events: %w", i, err)
		}
		for _, evt := range sourceInitEvents {
			encodeJobs <- evt
		}
	}

	sourceDone := make(chan error, len(a.sources))
	var sourceWG sync.WaitGroup
	for i, src := range a.sources {
		sourceWG.Add(1)
		go func(i int, src source.Source) {
			defer sourceWG.Done()
			err := src.Start(sourceCtx, func(evt *event.Event) error {
				select {
				case decodeJobs <- evt:
					return nil
				case <-sourceCtx.Done():
					return ctx.Err()
				}
			})
			sourceDone <- err
			if err != nil && sourceCtx.Err() == nil {
				a.logger.Error("source error", slog.Int("source_index", i), slog.String("error", err.Error()))
			}
		}(i, src)
	}

	shutdown := func() {
		// Stop stages from left to right so every downstream stage drains and gets
		// a chance to flush buffered or stateful output.
		close(decodeJobs)
		decodeWG.Wait()
		close(processJobs)
		processWG.Wait()
		if hasAggregators {
			close(aggregateRouteJobs)
			aggregateRouteWG.Wait()
			aggregateWG.Wait()
		}
		close(encodeJobs)
		encodeWG.Wait()
	}

	select {
	case err := <-sourceDone:
		stopSource()
		for _, src := range a.sources {
			_ = src.Close()
		}
		sourceWG.Wait()
		shutdown()
		if err != nil && sourceCtx.Err() == nil {
			return fmt.Errorf("run source: %w", err)
		}
		return nil
	case <-ctx.Done():
		stopSource()
		for _, src := range a.sources {
			_ = src.Close()
		}
		sourceWG.Wait()
		shutdown()
		return nil
	}
}

type aggregateWorker struct {
	cfg  config.AggregatorConfig
	agg  aggregate.Aggregator
	jobs chan *event.Event
}

// aggregatorMatches applies the optional aggregator.match filter to a processed event.
func aggregatorMatches(cfg config.AggregatorConfig, evt *event.Event) bool {
	if len(cfg.Match) == 0 {
		return true
	}
	for key, want := range cfg.Match {
		if eventMatchValue(evt, key) != want {
			return false
		}
	}
	return true
}

// eventMatchValue exposes a small set of envelope metadata alongside event fields
// so match rules can route on either transport metadata or canonical fields.
func eventMatchValue(evt *event.Event, key string) string {
	if evt == nil {
		return ""
	}
	if layer, ok := strings.CutPrefix(key, "packet.has_layer."); ok {
		if packetHasLayer(evt.Packet, layer) {
			return "true"
		}
		return "false"
	}
	switch key {
	case "stream":
		return evt.Stream
	case "kind":
		return evt.Kind
	case "source.type":
		return evt.Source.Type
	case "source.network":
		return evt.Source.Network
	case "source.address":
		return evt.Source.Address
	}
	if evt.Fields == nil {
		return ""
	}
	if val, ok := evt.Fields[key]; ok {
		return fmt.Sprint(val)
	}
	return ""
}

func packetHasLayer(packet *event.PacketModel, kind string) bool {
	if packet == nil || kind == "" {
		return false
	}
	for _, layer := range packet.Layers {
		if layer.Kind == kind {
			return true
		}
	}
	return false
}

// tickerChannel keeps select logic simple when a stage does not need timer-driven flushing.
func tickerChannel(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

// encoderTickInterval picks the smallest configured timer that should wake encoder workers.
func encoderTickInterval(cfg config.EncoderConfig) time.Duration {
	var min time.Duration
	add := func(ms int) {
		if ms <= 0 {
			return
		}
		d := time.Duration(ms) * time.Millisecond
		if min == 0 || d < min {
			min = d
		}
	}
	if cfg.Batch.Enabled {
		add(cfg.Batch.FlushInterval)
	}
	add(cfg.TemplateRefresh)
	add(cfg.OptionsRefresh)
	return min
}

// requiresOrderedExporter forces single-worker encoding for protocols whose
// packets, templates, or sequence numbers depend on ordering.
func requiresOrderedExporter(encoderType string) bool {
	switch encoderType {
	case "sflow", "ipfix", "netflowv9", "netflowv5", "pcap", "pcapng":
		return true
	default:
		return false
	}
}

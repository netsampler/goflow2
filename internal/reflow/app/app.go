package app

import (
	"context"
	"fmt"
	"log/slog"
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
	source           source.Source
	decoder          decode.Decoder
	processor        processor.Processor
	processorWorkers int
	aggregatorCfg    config.AggregatorConfig
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

	src, err := source.New(cfg.Source)
	if err != nil {
		return nil, fmt.Errorf("init source: %w", err)
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
		encoderCfg.SFlow.AgentIP = cfg.Sink.AgentIP
	}
	encoderWorkers := cfg.Encoder.Workers
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
		source:           src,
		decoder:          decode.New(),
		processor:        proc,
		processorWorkers: cfg.Processor.Workers,
		aggregatorCfg:    cfg.Aggregator,
		encoderCfg:       encoderCfg,
		encoderWorkers:   encoderWorkers,
		sink:             out,
	}, nil
}

// Run owns the source, processor, aggregation, encoding, and sink goroutines.
func (a *App) Run(ctx context.Context) error {
	a.logger.Info("starting ReFlow")
	defer a.sink.Close()

	sourceCtx, stopSource := context.WithCancel(ctx)
	defer stopSource()

	decodeJobs := make(chan *event.Event, a.processorWorkers*2)
	processJobs := make(chan *event.Event, a.processorWorkers*2)
	aggregateJobs := make(chan *event.Event, a.processorWorkers*2)
	encodeJobs := make(chan *event.Event, a.encoderWorkers*2)

	var decodeWG sync.WaitGroup
	decodeWG.Add(1)
	go func() {
		defer decodeWG.Done()
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
					aggregateJobs <- item
				}
			}
		}()
	}

	agg, err := aggregate.New(a.aggregatorCfg)
	if err != nil {
		return fmt.Errorf("init aggregator: %w", err)
	}

	var aggregateWG sync.WaitGroup
	aggregateWG.Add(1)
	go func() {
		defer aggregateWG.Done()
		var ticker *time.Ticker
		if interval := agg.Interval(); interval > 0 {
			ticker = time.NewTicker(interval)
			defer ticker.Stop()
		}
		forward := func(events []*event.Event) bool {
			for _, evt := range events {
				encodeJobs <- evt
			}
			return true
		}
		flush := func(closeStore bool) bool {
			var events []*event.Event
			var err error
			if closeStore {
				events, err = agg.Close()
			} else {
				events, err = agg.Flush()
			}
			if err != nil {
				a.logger.Error("aggregate flush error", slog.String("error", err.Error()))
				return true
			}
			return forward(events)
		}
		for {
			select {
			case <-tickerChannel(ticker):
				if !flush(false) {
					return
				}
			case evt, ok := <-aggregateJobs:
				if !ok {
					flush(true)
					return
				}
				events, err := agg.Process(evt)
				if err != nil {
					a.logger.Error("aggregate error", slog.String("error", err.Error()))
					continue
				}
				if !forward(events) {
					return
				}
			}
		}
	}()

	var encodeWG sync.WaitGroup
	for range a.encoderWorkers {
		encodeWG.Add(1)
		go func() {
			defer encodeWG.Done()
			enc, err := encode.New(a.encoderCfg)
			if err != nil {
				a.logger.Error("init encoder error", slog.String("error", err.Error()))
				stopSource()
				return
			}
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
		}()
	}

	initEvents, err := agg.InitEvents()
	if err != nil {
		return fmt.Errorf("init aggregator events: %w", err)
	}
	for _, evt := range initEvents {
		encodeJobs <- evt
	}
	sourceInitEvents, err := a.source.InitEvents()
	if err != nil {
		return fmt.Errorf("init source events: %w", err)
	}
	for _, evt := range sourceInitEvents {
		encodeJobs <- evt
	}

	sourceDone := make(chan error, 1)
	go func() {
		sourceDone <- a.source.Start(sourceCtx, func(evt *event.Event) error {
			select {
			case decodeJobs <- evt:
				return nil
			case <-sourceCtx.Done():
				return ctx.Err()
			}
		})
	}()

	shutdown := func() {
		close(decodeJobs)
		decodeWG.Wait()
		close(processJobs)
		processWG.Wait()
		close(aggregateJobs)
		aggregateWG.Wait()
		close(encodeJobs)
		encodeWG.Wait()
	}

	select {
	case err := <-sourceDone:
		shutdown()
		if err != nil && sourceCtx.Err() == nil {
			return fmt.Errorf("run source: %w", err)
		}
		return nil
	case <-ctx.Done():
		stopSource()
		_ = a.source.Close()
		<-sourceDone
		shutdown()
		return nil
	}
}

// tickerChannel keeps select logic simple when a stage does not need timer-driven flushing.
func tickerChannel(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

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

func requiresOrderedExporter(encoderType string) bool {
	switch encoderType {
	case "sflow", "ipfix", "netflowv9", "netflowv5":
		return true
	default:
		return false
	}
}

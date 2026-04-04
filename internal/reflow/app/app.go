package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/encode"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/internal/reflow/processor"
	"github.com/netsampler/goflow2/v3/internal/reflow/sink"
	"github.com/netsampler/goflow2/v3/internal/reflow/source/socket"
	"github.com/netsampler/goflow2/v3/pkg/goflow2/logging"
)

type App struct {
	logger           *slog.Logger
	source           *socket.Source
	processor        processor.Processor
	processorWorkers int
	encoder          encode.Encoder
	encoderWorkers   int
	sink             sink.Sink
}

func New(cfg *config.Config) (*App, error) {
	logger, err := logging.NewLogger(cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		return nil, fmt.Errorf("init logger: %w", err)
	}
	slog.SetDefault(logger)

	src, err := socket.New(cfg.Source)
	if err != nil {
		return nil, fmt.Errorf("init source: %w", err)
	}
	proc, err := processor.New(cfg.Processor)
	if err != nil {
		return nil, fmt.Errorf("init processor: %w", err)
	}
	enc, err := encode.New(cfg.Encoder)
	if err != nil {
		return nil, fmt.Errorf("init encoder: %w", err)
	}
	out, err := sink.New(cfg.Sink)
	if err != nil {
		return nil, fmt.Errorf("init sink: %w", err)
	}

	return &App{
		logger:           logger,
		source:           src,
		processor:        proc,
		processorWorkers: cfg.Processor.Workers,
		encoder:          enc,
		encoderWorkers:   cfg.Encoder.Workers,
		sink:             out,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	a.logger.Info("starting ReFlow")
	defer a.sink.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	processJobs := make(chan *event.Event, a.processorWorkers*2)
	encodeJobs := make(chan *event.Event, a.encoderWorkers*2)
	errCh := make(chan error, 1)

	var processWG sync.WaitGroup
	for range a.processorWorkers {
		processWG.Add(1)
		go func() {
			defer processWG.Done()
			for evt := range processJobs {
				events, err := a.processor.Process(evt)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return
				}
				for _, item := range events {
					select {
					case encodeJobs <- item:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	var encodeWG sync.WaitGroup
	for range a.encoderWorkers {
		encodeWG.Add(1)
		go func() {
			defer encodeWG.Done()
			for evt := range encodeJobs {
				payloads, err := a.encoder.Encode(evt)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return
				}
				for _, payload := range payloads {
					if err := a.sink.Send(payload); err != nil {
						a.logger.Error("sink write error", slog.String("error", err.Error()))
						continue
					}
				}
			}
		}()
	}

	sourceDone := make(chan error, 1)
	go func() {
		sourceDone <- a.source.Start(ctx, func(evt *event.Event) error {
			select {
			case processJobs <- evt:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	select {
	case err := <-errCh:
		close(processJobs)
		processWG.Wait()
		close(encodeJobs)
		encodeWG.Wait()
		return fmt.Errorf("run worker: %w", err)
	case err := <-sourceDone:
		close(processJobs)
		processWG.Wait()
		close(encodeJobs)
		encodeWG.Wait()
		if err != nil && ctx.Err() == nil {
			return fmt.Errorf("run source: %w", err)
		}
		return nil
	}
}

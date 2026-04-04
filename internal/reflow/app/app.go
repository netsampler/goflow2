package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

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
	encoderCfg       config.EncoderConfig
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
	out, err := sink.New(cfg.Sink)
	if err != nil {
		return nil, fmt.Errorf("init sink: %w", err)
	}

	return &App{
		logger:           logger,
		source:           src,
		processor:        proc,
		processorWorkers: cfg.Processor.Workers,
		encoderCfg:       cfg.Encoder,
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
			enc, err := encode.New(a.encoderCfg)
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				cancel()
				return
			}
			var ticker *time.Ticker
			if a.encoderCfg.Batch.Enabled && a.encoderCfg.Batch.FlushInterval > 0 {
				ticker = time.NewTicker(time.Duration(a.encoderCfg.Batch.FlushInterval) * time.Millisecond)
				defer ticker.Stop()
			}
			flush := func() bool {
				payloads, err := enc.Flush()
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return false
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
				case <-ctx.Done():
					flush()
					return
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
						}
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

func tickerChannel(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

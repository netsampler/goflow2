package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/netsampler/goflow2/v3/metrics"
	"github.com/netsampler/goflow2/v3/pkg/goflow2/builder"
	"github.com/netsampler/goflow2/v3/pkg/goflow2/collector"
	"github.com/netsampler/goflow2/v3/pkg/goflow2/config"
	"github.com/netsampler/goflow2/v3/pkg/goflow2/httpserver"
	"github.com/netsampler/goflow2/v3/pkg/goflow2/listen"
	"github.com/netsampler/goflow2/v3/pkg/goflow2/logging"
	"github.com/netsampler/goflow2/v3/utils/debug"
	"github.com/netsampler/goflow2/v3/utils/store/persistence"
	"github.com/netsampler/goflow2/v3/utils/store/samplingrate"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
)

// App wires and runs the GoFlow2 application.
type App struct {
	cfg         *config.Config
	logger      *slog.Logger
	collector   *collector.Collector
	persistence *persistence.Manager
	transport   interface{ Close() error }
	producer    interface{ Close() }
	server      *http.Server
	serverErr   chan error
	collecting  atomic.Bool
}

// New constructs a new App from config.
func New(cfg *config.Config) (*App, error) {
	logger, err := logging.NewLogger(cfg.LogLevel, cfg.LogFmt)
	if err != nil {
		return nil, fmt.Errorf("app: init logger: %w", err)
	}
	slog.SetDefault(logger)

	formatter, err := builder.BuildFormatter(cfg.Format)
	if err != nil {
		return nil, fmt.Errorf("app: build formatter: %w", err)
	}
	transporter, err := builder.BuildTransport(cfg.Transport)
	if err != nil {
		return nil, fmt.Errorf("app: build transport: %w", err)
	}

	persist := persistence.New(persistence.Config{
		Path:     cfg.StoreJSONPath,
		Interval: cfg.StoreJSONInterval,
	})

	samplingStore, err := persist.NewSamplingRateStore(
		samplingrate.WithTTL(cfg.SamplingRatesTTL),
		samplingrate.WithExtendOnAccess(cfg.SamplingRatesExtendOnAccess),
		samplingrate.WithSweepInterval(cfg.SamplingRatesSweepInterval),
		samplingrate.WithHooks(metrics.SamplingRateStoreHooks()),
	)
	if err != nil {
		return nil, fmt.Errorf("app: init sampling persistence: %w", err)
	}
	templateStore, err := persist.NewTemplateStore(
		templates.WithTTL(cfg.TemplatesTTL),
		templates.WithExtendOnAccess(cfg.TemplatesExtendOnAccess),
		templates.WithSweepInterval(cfg.TemplatesSweepInterval),
		templates.WithHooks(metrics.TemplateStoreHooks()),
	)
	if err != nil {
		return nil, fmt.Errorf("app: init template persistence: %w", err)
	}

	flowProducer, err := builder.BuildProducer(cfg, samplingStore)
	if err != nil {
		return nil, fmt.Errorf("app: build producer: %w", err)
	}

	flowProducer = debug.WrapPanicProducer(flowProducer)
	flowProducer = metrics.WrapPromProducer(flowProducer)

	listeners, err := listen.ParseListenAddresses(cfg.ListenAddresses)
	if err != nil {
		return nil, fmt.Errorf("app: parse listen addresses: %w", err)
	}

	coll, err := collector.New(collector.Config{
		Listeners:     listeners,
		Formatter:     formatter,
		Transport:     transporter,
		Producer:      flowProducer,
		TemplateStore: templateStore,
		ErrCnt:        cfg.ErrCnt,
		ErrInt:        cfg.ErrInt,
		Logger:        logger,
	})
	if err != nil {
		return nil, fmt.Errorf("app: init collector: %w", err)
	}

	app := &App{
		cfg:         cfg,
		logger:      logger,
		collector:   coll,
		persistence: persist,
		transport:   transporter,
		producer:    flowProducer,
		serverErr:   make(chan error, 1),
	}

	if cfg.Addr != "" {
		mux := httpserver.New(httpserver.Config{
			Addr:          cfg.Addr,
			StoreHTTPPath: cfg.StoreHTTPPath,
		}, persist.Document, app.collecting.Load)
		app.server = &http.Server{
			Addr:              cfg.Addr,
			Handler:           mux,
			ReadHeaderTimeout: time.Second * 5,
		}
	}

	return app, nil
}

// Start starts the collector and HTTP server.
func (a *App) Start() error {
	a.logger.Info("starting GoFlow2")

	a.persistence.Start()
	go func() {
		for err := range a.persistence.Errors() {
			a.logger.Error("flowstore persistence error", slog.String("error", err.Error()))
		}
	}()

	if err := a.collector.Start(); err != nil {
		a.persistence.Close()
		return fmt.Errorf("app: start collector: %w", err)
	}
	a.collecting.Store(true)

	if a.server == nil {
		return nil
	}

	go func() {
		err := a.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.serverErr <- fmt.Errorf("http server: %w", err)
			return
		}
		a.logger.With(slog.String("http", a.cfg.Addr)).Info("closed HTTP server")
	}()

	return nil
}

// Run starts the app and blocks until context cancellation or server error.
func (a *App) Run(ctx context.Context) error {
	if err := a.Start(); err != nil {
		return fmt.Errorf("app run start: %w", err)
	}

	if a.server == nil {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		a.Shutdown(shutdownCtx)
		cancel()
		return nil
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		a.Shutdown(shutdownCtx)
		cancel()
		return nil
	case err := <-a.Wait():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		a.Shutdown(shutdownCtx)
		cancel()
		return fmt.Errorf("app run wait: %w", err)
	}
}

// Wait returns a channel that receives HTTP server errors.
func (a *App) Wait() <-chan error {
	return a.serverErr
}

// Shutdown stops receivers, closes transports, and shuts down the HTTP server.
func (a *App) Shutdown(ctx context.Context) {
	a.collecting.Store(false)

	a.collector.Stop()
	a.producer.Close()
	a.persistence.Close()
	if err := a.transport.Close(); err != nil {
		a.logger.Error("error closing transport", slog.String("error", err.Error()))
	}
	a.logger.Info("transporter closed")

	if a.server == nil {
		return
	}
	if err := a.server.Shutdown(ctx); err != nil {
		a.logger.Error("error shutting-down HTTP server", slog.String("error", err.Error()))
	}
}

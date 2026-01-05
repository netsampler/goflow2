package app

import (
	"context"
	"errors"
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
)

// App wires and runs the GoFlow2 application.
type App struct {
	cfg        *config.Config
	logger     *slog.Logger
	collector  *collector.Collector
	transport  interface{ Close() error }
	producer   interface{ Close() }
	server     *http.Server
	serverErr  chan error
	collecting atomic.Bool
}

// New constructs a new App from config.
func New(cfg *config.Config) (*App, error) {
	logger, err := logging.NewLogger(cfg.LogLevel, cfg.LogFmt)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)

	formatter, err := builder.BuildFormatter(cfg.Format)
	if err != nil {
		return nil, err
	}
	transporter, err := builder.BuildTransport(cfg.Transport)
	if err != nil {
		return nil, err
	}
	flowProducer, err := builder.BuildProducer(cfg)
	if err != nil {
		return nil, err
	}

	flowProducer = debug.WrapPanicProducer(flowProducer)
	flowProducer = metrics.WrapPromProducer(flowProducer)

	listeners, err := listen.ParseListenAddresses(cfg.ListenAddresses)
	if err != nil {
		return nil, err
	}

	coll, err := collector.New(collector.Config{
		Listeners:               listeners,
		Formatter:               formatter,
		Transport:               transporter,
		Producer:                flowProducer,
		ErrCnt:                  cfg.ErrCnt,
		ErrInt:                  cfg.ErrInt,
		Logger:                  logger,
		TemplatesTTL:            cfg.TemplatesTTL,
		TemplatesSweepInterval:  cfg.TemplatesSweepInterval,
		TemplatesExtendOnAccess: cfg.TemplatesExtendOnAccess,
		TemplatesJSONPath:       cfg.TemplatesJSONPath,
		TemplatesJSONInterval:   cfg.TemplatesJSONInterval,
	})
	if err != nil {
		return nil, err
	}

	app := &App{
		cfg:       cfg,
		logger:    logger,
		collector: coll,
		transport: transporter,
		producer:  flowProducer,
		serverErr: make(chan error, 1),
	}

	if cfg.Addr != "" {
		mux := httpserver.New(httpserver.Config{
			Addr:         cfg.Addr,
			TemplatePath: cfg.TemplatePath,
		}, coll.NetFlowTemplates, app.collecting.Load)
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

	if err := a.collector.Start(); err != nil {
		return err
	}
	a.collecting.Store(true)

	if a.server == nil {
		return nil
	}

	go func() {
		err := a.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.serverErr <- err
			return
		}
		a.logger.With(slog.String("http", a.cfg.Addr)).Info("closed HTTP server")
	}()

	return nil
}

// Run starts the app and blocks until context cancellation or server error.
func (a *App) Run(ctx context.Context) error {
	if err := a.Start(); err != nil {
		return err
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
		return err
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

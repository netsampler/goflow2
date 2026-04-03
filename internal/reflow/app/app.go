package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/internal/reflow/output"
	"github.com/netsampler/goflow2/v3/internal/reflow/source/socket"
	"github.com/netsampler/goflow2/v3/internal/reflow/transform"
	"github.com/netsampler/goflow2/v3/pkg/goflow2/logging"
)

type App struct {
	logger     *slog.Logger
	source     *socket.Source
	transforms []transform.Transform
	output     *output.Writer
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
	chain, err := transform.BuildChain(cfg.Transforms)
	if err != nil {
		return nil, fmt.Errorf("build transforms: %w", err)
	}
	out, err := output.New(cfg.Output)
	if err != nil {
		return nil, fmt.Errorf("init output: %w", err)
	}

	return &App{
		logger:     logger,
		source:     src,
		transforms: chain,
		output:     out,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	a.logger.Info("starting ReFlow")
	defer a.output.Close()

	err := a.source.Start(ctx, func(evt *event.Event) error {
		events, err := transform.ApplyChain(a.transforms, evt)
		if err != nil {
			return err
		}
		for _, item := range events {
			if err := a.output.Write(item); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("run source: %w", err)
	}
	return nil
}

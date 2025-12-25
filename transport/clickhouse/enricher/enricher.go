package enricher

//go:generate go run gen.go

import (
	"context"
	"errors"
	"log/slog"

	"github.com/netsampler/goflow2/v2/transport/clickhouse/common"
	"golang.org/x/sync/errgroup"
)

type metaEnricher struct {
	enricherDrivers []common.Enricher
}

func New(enrichers []string) (common.Enricher, error) {
	metaEnricher := metaEnricher{}
	e, err := common.Enrichers(enrichers)
	if err != nil {
		return nil, err
	}
	metaEnricher.enricherDrivers = e
	return &metaEnricher, nil
}

func (d *metaEnricher) Init(ctx context.Context) error {
	wg, runCtx := errgroup.WithContext(ctx)
	for _, e := range d.enricherDrivers {
		slog.Info("initializing enricher", slog.String("name", e.Name()))
		wg.Go(func() error { return e.Init(runCtx) })
	}
	return wg.Wait()
}

func (d *metaEnricher) Name() string {
	return "meta"
}

func (d *metaEnricher) Proceed(record common.EnrichmentRecord) error {
	var proceedErr error
	for _, e := range d.enricherDrivers {
		proceedErr = errors.Join(e.Proceed(record), proceedErr)
	}
	return proceedErr
}

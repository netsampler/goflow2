package clickhouse

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	metrics "github.com/netsampler/goflow2/v2/metrics"
	flowpb "github.com/netsampler/goflow2/v2/pb"
	"github.com/netsampler/goflow2/v2/transport"
	"github.com/netsampler/goflow2/v2/transport/clickhouse/common"
	"github.com/netsampler/goflow2/v2/transport/clickhouse/common/ticker"
	"github.com/netsampler/goflow2/v2/transport/clickhouse/config"
	"github.com/netsampler/goflow2/v2/transport/clickhouse/db"
	"github.com/netsampler/goflow2/v2/transport/clickhouse/enricher"
	"github.com/netsampler/goflow2/v2/transport/clickhouse/queue"
	"google.golang.org/protobuf/encoding/protodelim"
)

const (
	InitTimeout           = 60 * time.Second
	TransportCloseTimeout = 5 * time.Second
)

var (
	pool = sync.Pool{
		New: func() any {
			return new(bytes.Buffer)
		},
	}
)

type ClickhouseDriver struct {
	configResolver config.ConfigResolver
	dbConfig       config.DbConfig

	queue        *queue.SizeLimitedQueueFIFO[queue.MessageWrapper]
	dbConnection *db.DB
	enricher     common.Enricher
	metrics      *metrics.Metric
	bgErrGroup   *errgroup.Group
	errors       chan error
	logger       *slog.Logger
}

func (d *ClickhouseDriver) Prepare() error {
	var err error
	d.configResolver, err = config.Prepare(flag.CommandLine)
	if err != nil {
		return fmt.Errorf("failed to prepare config %w", err)
	}

	d.metrics, err = metrics.GetOrCreate("ch")
	if err != nil {
		return fmt.Errorf("failed to create metrics, %w", err)
	}
	d.queue = queue.NewSizeLimitedQueueFIFO[queue.MessageWrapper](queue.QueueSettings{
		MetricAvailableMemory: d.metrics.Metric("queue_mem_avail"),
		MetricUsedMemory:      d.metrics.Metric("queue_mem_used"),
		MetricQueuedRecords:   d.metrics.Metric("queue_records_queued"),
		MetricDroppedRecords:  d.metrics.Counter("queue_records_dropped"),
	})
	d.logger = slog.With("transport", "clickhouse")
	return nil
}

func (d *ClickhouseDriver) Errors() <-chan error {
	return d.errors
}

func (d *ClickhouseDriver) Init() error {
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), InitTimeout)
	defer cancel()
	d.initEnrichers(ctx)

	d.dbConfig = d.configResolver.Database()
	d.logger.Info("creating database connection")
	d.dbConnection, err = db.NewDB(d.dbConfig, d.enricher, d.metrics)
	if err != nil {
		return err
	}

	var bgCtx context.Context
	d.bgErrGroup, bgCtx = errgroup.WithContext(context.Background())
	d.bgErrGroup.Go(func() error {
		return d.StartInsertLoop(bgCtx)
	})
	d.bgErrGroup.Go(func() error {
		return ticker.Run(bgCtx, 30*time.Minute, 10*time.Minute, d.enricher.Init)
	})

	return nil
}

func (d *ClickhouseDriver) Send(key, data []byte) error {
	msg := flowpb.FlowMessage{}
	buf := pool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.Write(data)
	defer pool.Put(buf)
	if err := protodelim.UnmarshalFrom(buf, &msg); err != nil {
		d.logger.Error("failed to unmarshall flow record", slog.String("error", err.Error()))
		d.metrics.Counter("unmarshall_err").Inc()
		return err
	}

	if err := d.queue.Add(queue.Wrap(&msg, uint(len(data)))); err != nil {
		d.metrics.Counter("queue_add_err").Inc()
		d.logger.Error("failed to add record to queue", slog.String("error", err.Error()))
		return err
	}
	return nil
}

func (d *ClickhouseDriver) StartInsertLoop(ctx context.Context) error {
	//make counter visible in prometheus
	d.metrics.Counter("insert_err").Add(0)
	insert := func() error {
		defer metrics.TimeMeasureNow().MeasureTime(d.metrics.Metric("insert_time"))

		insertCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		log := d.logger.With("id", common.ShortID())
		log.Info("begin saving")
		processedRecords, err := d.dbConnection.AddRecords(insertCtx, log, d.queue)
		if err != nil {
			d.metrics.Counter("insert_err").Add(float64(processedRecords))
			log.Error("failed to save records", slog.Int("numberOfRecords", processedRecords), slog.String("error", err.Error()))
			return fmt.Errorf("failed to save records, %v", err)
		}
		log.Info("end saving")
		return nil
	}

	attempts := 0
	insertTicker := time.NewTicker(d.dbConfig.InsertInterval)
	defer insertTicker.Stop()
	minBatchSizeThreshold := int(float64(d.dbConnection.BatchSize()) * d.dbConfig.BatchInsertThreshold)
	sem := semaphore.NewWeighted(int64(d.dbConfig.MaxParallelInserts))
	for {
		_, ok := <-insertTicker.C
		if !ok {
			d.logger.Error("insert loop terminated unexpectedly")
			d.metrics.Status("insert_loop", metrics.StatusCrit)
			return fmt.Errorf("insert ticker is stopped")
		}
		queueLength := d.queue.Length()
		if queueLength == 0 {
			continue
		}
		if queueLength < minBatchSizeThreshold && attempts < d.dbConfig.AttemptsBeforeForceInsert {
			attempts++
			continue
		}
		if sem.TryAcquire(1) {
			d.metrics.Counter("insert_job_runned").Inc()
			go func() {
				defer sem.Release(1)
				insert()
			}()
		} else {
			d.metrics.Counter("insert_job_skipped_err").Inc()
		}
		attempts = 0
	}
}

func (d *ClickhouseDriver) Close() error {
	done := make(chan error)
	go func() {
		done <- d.bgErrGroup.Wait()
		close(done)
	}()
	select {
	case err := <-done:
		d.logger.Info("all goroutines gracefully stopped")
		return err
	case <-time.After(TransportCloseTimeout):
		d.logger.Info("timeout waiting for goroutines, force closing")
		return nil
	}
}

func (d *ClickhouseDriver) initEnrichers(ctx context.Context) error {
	var err error
	d.logger.Info("initialize enrichers")
	d.enricher, err = enricher.New(d.configResolver.Enrichers())
	if err != nil {
		return fmt.Errorf("failed to initialize enricher %w", err)
	}
	if err := d.enricher.Init(ctx); err != nil {
		return fmt.Errorf("failed to initialize enrichers, %w", err)
	}
	d.logger.Info("enrichers are ready")
	return nil
}

func init() {
	d := &ClickhouseDriver{
		errors: make(chan error, 10),
	}
	transport.RegisterTransportDriver("clickhouse", d)
}

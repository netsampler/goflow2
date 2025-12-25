package db

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	metrics "github.com/netsampler/goflow2/v2/metrics"
	flowpb "github.com/netsampler/goflow2/v2/pb"
	"github.com/netsampler/goflow2/v2/transport/clickhouse/common"
	"github.com/netsampler/goflow2/v2/transport/clickhouse/config"
	"github.com/netsampler/goflow2/v2/transport/clickhouse/queue"
)

const (
	FlowInsert = "INSERT INTO flows_local"

	DialTimeout          = 3 * time.Second
	ConnMaxLifetime      = 10 * time.Minute
	BlockBufferSize      = 20
	MaxCompressionBuffer = 10240
	MaxExecutionTime     = 60
)

type DB struct {
	connPool  *ConnPool
	batchSize int
	settings  config.DbConfig
	fields    []string
	enricher  common.Enricher
	inserSQL  string
	metrics   *metrics.Metric
}

func NewDB(s config.DbConfig, enricher common.Enricher, metrics *metrics.Metric) (*DB, error) {
	db := DB{
		settings: s, batchSize: s.BatchSize,
		enricher: enricher,
		metrics:  metrics,
	}
	db.prepareInsertSQL()
	err := db.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database server, %w", err)
	}

	return &db, nil
}

func (d *DB) prepareInsertSQL() {
	record := RecordFromFlowMessage(&flowpb.FlowMessage{})
	d.enricher.Proceed(record)
	d.fields = record.Fields()
	slog.Info("database fields", slog.Any("fields", d.fields))
	d.inserSQL = FlowInsert + "(" + strings.Join(d.fields, ",") + ") VALUES"
}

func (d *DB) BatchSize() int {
	return d.batchSize
}

func (d *DB) Connect() error {
	opts := ch.Options{
		Protocol: ch.Native,
		Addr:     d.settings.Srv,
		Auth: ch.Auth{
			Database: d.settings.Database,
			Username: d.settings.User,
			Password: d.settings.Password.Expose(),
		},
		TLS: &tls.Config{
			InsecureSkipVerify: true,
		},
		Settings: ch.Settings{
			"max_execution_time": MaxExecutionTime,
		},
		Compression: &ch.Compression{
			Method: ch.CompressionLZ4,
		},
		DialTimeout:          DialTimeout,
		ConnMaxLifetime:      ConnMaxLifetime,
		BlockBufferSize:      BlockBufferSize,
		MaxCompressionBuffer: MaxCompressionBuffer,
		ClientInfo: ch.ClientInfo{
			Products: []struct {
				Name    string
				Version string
			}{
				{Name: metrics.NAMESPACE, Version: "0.1"},
			},
		},
	}

	var err error
	d.connPool, err = NewConnPool(d.settings.Cluster, &opts)
	if err != nil {
		return fmt.Errorf("failed to create connection pool, %v", err)
	}
	return nil
}

func (d *DB) RandomServerConnection(log *slog.Logger) (ch.Conn, error) {
	return d.connPool.RandomServerConnection(log)
}

func (d *DB) AddRecords(ctx context.Context, log *slog.Logger, queue *queue.SizeLimitedQueueFIFO[queue.MessageWrapper]) (int, error) {
	conn, err := d.connPool.RandomServerConnection(log)
	if err != nil {
		return 0, fmt.Errorf("failed to get connection, %w", err)
	}

	log.Info("prepare batch")
	batch, err := conn.PrepareBatch(ctx, d.inserSQL)
	if err != nil {
		return 0, err
	}

	log.Info("populate the batch with records")
	var processedRecords int
	for ; processedRecords < d.batchSize; processedRecords++ {
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("die while processing records, total processed %d, %w", processedRecords, ctx.Err())
		default:
		}

		wrappedMsg, hasData := queue.Get()
		if !hasData {
			break
		}
		r := wrappedMsg.Message()
		if r == nil {
			log.Error("wrapped message is nil, break processing")
			break
		}
		record := RecordFromFlowMessage(r)
		err = d.enricher.Proceed(record)
		if err != nil {
			d.metrics.Counter("proceed_err").Inc()
			continue
		}

		err = batch.Append(record.Values(d.fields)...)
		if err != nil {
			log.Error("failed to append record to batch, add back to queue", slog.String("err", err.Error()))
			queue.Add(wrappedMsg)
			break
		}
		recordsPool.Put(record)
	}
	log.Info("batch is ready, send it")
	err = batch.Send()
	if err != nil {
		return processedRecords, fmt.Errorf("failed to send batch, %w", err)
	}
	log.Info("the batch has been sent", slog.Int("records", processedRecords))
	d.metrics.Metric("batch_size").Set(float64(processedRecords))
	return processedRecords, err
}

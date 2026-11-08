package main

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/netsampler/goflow2/v3/pkg/reflow/app"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

func newMeterProvider(ctx context.Context) (*sdkmetric.MeterProvider, error) {
	switch exporter := os.Getenv("OTEL_METRICS_EXPORTER"); exporter {
	case "", "none":
		return nil, nil
	case "otlp":
	default:
		return nil, fmt.Errorf("unsupported OTEL_METRICS_EXPORTER %q", exporter)
	}
	protocol := os.Getenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL")
	if protocol == "" {
		protocol = os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	}
	if protocol != "" && protocol != "http/protobuf" {
		return nil, fmt.Errorf("unsupported OTLP metrics protocol %q", protocol)
	}
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attribute.String("service.name", "reflow")),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, err
	}
	exporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	return sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
	), nil
}

func registerMetrics(provider *sdkmetric.MeterProvider, observed *atomic.Pointer[app.App], timing bool) error {
	meter := provider.Meter("github.com/netsampler/goflow2/v3/pkg/reflow")
	var sourceEvents, captureDrops, decodedEvents, aggregateRecords, stageErrors, sinkPayloads, sinkBytes metric.Int64ObservableCounter
	var timingCount metric.Int64ObservableCounter
	var timingSum metric.Float64ObservableCounter
	var instruments []metric.Observable

	type counterSpec struct {
		instrument              *metric.Int64ObservableCounter
		name, unit, description string
	}
	counters := []counterSpec{
		{&sourceEvents, "reflow.source.events", "{event}", "Source events accepted into the decode queue"},
		{&decodedEvents, "reflow.decode.events", "{event}", "Events emitted by the decoder"},
		{&aggregateRecords, "reflow.aggregate.records", "{record}", "Records emitted by aggregation, including on shutdown"},
		{&stageErrors, "reflow.stage.errors", "{error}", "Pipeline stage errors"},
		{&sinkPayloads, "reflow.sink.payloads", "{payload}", "Encoded payloads successfully written by the sink"},
		{&sinkBytes, "reflow.sink.bytes", "By", "Encoded payload bytes successfully written by the sink"},
		{&captureDrops, "reflow.source.capture.drops", "{packet}", "Capture delivery losses reported by the source"},
	}
	if timing {
		counters = append(counters, counterSpec{&timingCount, "reflow.stage.duration.observations", "{operation}", "Number of timed pipeline stage calls"})
		var err error
		timingSum, err = meter.Float64ObservableCounter("reflow.stage.duration.sum", metric.WithUnit("s"),
			metric.WithDescription("Total elapsed time across timed pipeline stage calls"))
		if err != nil {
			return err
		}
		instruments = append(instruments, timingSum)
	}
	for _, counter := range counters {
		instrument, err := meter.Int64ObservableCounter(counter.name, metric.WithUnit(counter.unit), metric.WithDescription(counter.description))
		if err != nil {
			return err
		}
		*counter.instrument = instrument
		instruments = append(instruments, instrument)
	}
	queueDepth, err := meter.Int64ObservableGauge("reflow.queue.depth", metric.WithUnit("{event}"),
		metric.WithDescription("Current number of events in a pipeline queue"))
	if err != nil {
		return err
	}
	queueCapacity, err := meter.Int64ObservableGauge("reflow.queue.capacity", metric.WithUnit("{event}"),
		metric.WithDescription("Maximum number of events in a pipeline queue"))
	if err != nil {
		return err
	}
	instruments = append(instruments, queueDepth, queueCapacity)

	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		application := observed.Load()
		if application == nil {
			return nil
		}
		stats := application.Stats()
		for i, source := range stats.Sources {
			attrs := metric.WithAttributes(attribute.Int("source.index", i))
			if source.Events > 0 {
				o.ObserveInt64(sourceEvents, source.Events, attrs)
			}
			if source.CaptureDrops > 0 {
				o.ObserveInt64(captureDrops, source.CaptureDrops, attrs)
			}
		}
		if stats.DecodedEvents > 0 {
			o.ObserveInt64(decodedEvents, stats.DecodedEvents)
		}
		if stats.AggregateRecords > 0 {
			o.ObserveInt64(aggregateRecords, stats.AggregateRecords)
		}
		if stats.SinkPayloads > 0 {
			o.ObserveInt64(sinkPayloads, stats.SinkPayloads)
			o.ObserveInt64(sinkBytes, stats.SinkBytes)
		}
		for operation, count := range stats.Errors {
			o.ObserveInt64(stageErrors, count, operationAttrs(operation))
		}
		for operation, duration := range stats.Durations {
			attrs := operationAttrs(operation)
			o.ObserveInt64(timingCount, duration.Count, attrs)
			o.ObserveFloat64(timingSum, duration.Total.Seconds(), attrs)
		}
		for _, queue := range stats.Queues {
			labels := []attribute.KeyValue{attribute.String("queue", queue.Name)}
			if queue.AggregatorIndex >= 0 {
				labels = append(labels, attribute.Int("aggregator.index", queue.AggregatorIndex))
			}
			attrs := metric.WithAttributes(labels...)
			o.ObserveInt64(queueDepth, int64(queue.Depth), attrs)
			o.ObserveInt64(queueCapacity, int64(queue.Capacity), attrs)
		}
		return nil
	}, instruments...)
	return err
}

func operationAttrs(operation app.StageOperation) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("stage", operation.Stage), attribute.String("operation", operation.Operation))
}

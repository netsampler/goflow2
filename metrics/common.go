package metrics

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/prometheus/common/expfmt"
)

type StatusType int64

const (
	StatusOK StatusType = iota
	StatusWarn
	StatusErr
	StatusCrit
)

var metricsRegistry sync.Map

type Metric struct {
	system  string
	service string
	status  *prometheus.GaugeVec
	gauge   *prometheus.GaugeVec
	counter *prometheus.CounterVec
}

func GetOrCreate(name string) (*Metric, error) {
	e, ok := metricsRegistry.Load(name)
	if ok {
		return e.(*Metric), nil
	}
	newMetric := Metric{
		system:  name,
		service: NAMESPACE,
		status: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name:      "status",
				Subsystem: name,
				Namespace: NAMESPACE,
			}, []string{"system", "status", "service"}),
		gauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name:      "metric",
				Subsystem: name,
				Namespace: NAMESPACE,
			}, []string{"system", "metric", "service"}),
		counter: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name:      "counter",
				Subsystem: name,
				Namespace: NAMESPACE,
			}, []string{"system", "counter", "service"}),
	}
	e, ok = metricsRegistry.LoadOrStore(name, &newMetric)
	if !ok {
		err := newMetric.register()
		if err != nil {
			return nil, err
		}
	}
	return e.(*Metric), nil
}

func (e *Metric) Push(uri string) error {
	err := push.New(uri, "push").
		Collector(e.counter).
		Collector(e.gauge).
		Collector(e.status).
		Format(expfmt.NewFormat(expfmt.TypeTextPlain)).
		Push()
	if err != nil {
		return fmt.Errorf("could not push metrics, %w", err)
	}
	return nil
}

func (e *Metric) Status(name string, status StatusType) {
	e.status.With(prometheus.Labels{"system": e.system, "service": e.service, "status": name}).Set(float64(status))
}

func (e *Metric) Metric(name string) prometheus.Gauge {
	return e.gauge.With(prometheus.Labels{"system": e.system, "service": e.service, "metric": name})
}

func (e *Metric) Counter(name string) prometheus.Counter {
	return e.counter.With(prometheus.Labels{"system": e.system, "service": e.service, "counter": name})
}

func (e *Metric) register() error {
	if err := prometheus.Register(e.status); err != nil {
		return err
	}
	if err := prometheus.Register(e.gauge); err != nil {
		return err
	}
	if err := prometheus.Register(e.counter); err != nil {
		return err
	}
	return nil
}

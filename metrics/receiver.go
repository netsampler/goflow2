package metrics

import (
	"strconv"

	"github.com/netsampler/goflow2/v2/utils"

	"github.com/prometheus/client_golang/prometheus"
)

// ReceiverMetric records packet drop metrics.
type ReceiverMetric struct{}

// NewReceiverMetric creates a ReceiverMetric instance.
func NewReceiverMetric() *ReceiverMetric {
	return &ReceiverMetric{}
}

// Dropped records a dropped packet metric.
func (r *ReceiverMetric) Dropped(pkt utils.Message) {
	remote := pkt.Src.Addr().Unmap().String()
	localIP := pkt.Dst.Addr().Unmap().String()

	port := strconv.FormatUint(uint64(pkt.Dst.Port()), 10)
	size := len(pkt.Payload)

	labels := prometheus.Labels{
		"remote_ip":  remote,
		"local_ip":   localIP,
		"local_port": port,
	}
	MetricReceivedDroppedPackets.With(labels).Inc()
	MetricReceivedDroppedBytes.With(labels).Add(float64(size))
}

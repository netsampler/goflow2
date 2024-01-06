package metrics

import (
	"fmt"

	"github.com/netsampler/goflow2/v2/utils"

	"github.com/prometheus/client_golang/prometheus"
)

type ReceiverMetric struct {
}

func NewReceiverMetric() *ReceiverMetric {
	return &ReceiverMetric{}
}

func (r *ReceiverMetric) Dropped(pkt utils.Message) {
	remote := pkt.Src.Addr().Unmap().String()
	localIP := pkt.Dst.Addr().Unmap().String()

	port := fmt.Sprintf("%d", pkt.Dst.Port())
	size := len(pkt.Payload)

	labels := prometheus.Labels{
		"remote_ip":  remote,
		"local_ip":   localIP,
		"local_port": port,
	}
	MetricReceivedDroppedPackets.With(labels).Inc()
	MetricReceivedDroppedBytes.With(labels).Add(float64(size))
}

package metrics

import (
	"errors"
	"fmt"
	"time"

	"github.com/netsampler/goflow2/decoders/netflow"
	"github.com/netsampler/goflow2/utils"

	"github.com/prometheus/client_golang/prometheus"
)

func PromDecoderWrapper(wrapped utils.DecoderFunc, name string) utils.DecoderFunc {
	return func(msg interface{}) error {
		pkt, ok := msg.(*utils.Message)
		if !ok {
			return fmt.Errorf("flow is not *Message")
		}
		remote := pkt.Src.Addr().String()
		localIP := pkt.Dst.Addr().String()
		port := fmt.Sprintf("%d", pkt.Dst.Port())
		size := len(pkt.Payload)

		MetricTrafficBytes.With(
			prometheus.Labels{
				"remote_ip":  remote,
				"local_ip":   localIP,
				"local_port": port,
				"type":       name,
			}).
			Add(float64(size))
		MetricTrafficPackets.With(
			prometheus.Labels{
				"remote_ip":  remote,
				"local_ip":   localIP,
				"local_port": port,
				"type":       name,
			}).
			Inc()
		MetricPacketSizeSum.With(
			prometheus.Labels{
				"remote_ip":  remote,
				"local_ip":   localIP,
				"local_port": port,
				"type":       name,
			}).
			Observe(float64(size))

		timeTrackStart := time.Now().UTC()

		err := wrapped(msg)

		timeTrackStop := time.Now().UTC()

		DecoderTime.With(
			prometheus.Labels{
				"name": name,
			}).
			Observe(float64((timeTrackStop.Sub(timeTrackStart)).Nanoseconds()) / 1000)

		if err != nil {
			if errors.Is(err, netflow.ErrorTemplateNotFound) {
				NetFlowErrors.With(
					prometheus.Labels{
						"router": remote,
						"error":  "template_not_found",
					}).
					Inc()
			}

			switch err.(type) {
			default:
				// FIXME
				NetFlowErrors.With(
					prometheus.Labels{
						"router": remote,
						"error":  "error_decoding",
					}).
					Inc()
				SFlowErrors.With(
					prometheus.Labels{
						"router": remote,
						"error":  "error_decoding",
					}).
					Inc()
			}
		}
		return err
	}
}

func recordCommonNetFlowMetrics(version uint16, key string, flowSets []interface{}) {
	versionStr := fmt.Sprintf("%d", version)
	NetFlowStats.With(
		prometheus.Labels{
			"router":  key,
			"version": versionStr,
		}).
		Inc()

	for _, fs := range flowSets {
		switch fsConv := fs.(type) {
		case netflow.TemplateFlowSet:
			NetFlowSetStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": versionStr,
					"type":    "TemplateFlowSet",
				}).
				Inc()

			NetFlowSetRecordsStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": versionStr,
					"type":    "TemplateFlowSet",
				}).
				Add(float64(len(fsConv.Records)))
		case netflow.NFv9OptionsTemplateFlowSet:
			NetFlowSetStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": versionStr,
					"type":    "OptionsTemplateFlowSet",
				}).
				Inc()

			NetFlowSetRecordsStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": versionStr,
					"type":    "OptionsTemplateFlowSet",
				}).
				Add(float64(len(fsConv.Records)))
		case netflow.IPFIXOptionsTemplateFlowSet:
			NetFlowSetStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": versionStr,
					"type":    "OptionsTemplateFlowSet",
				}).
				Inc()

			NetFlowSetRecordsStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": versionStr,
					"type":    "OptionsTemplateFlowSet",
				}).
				Add(float64(len(fsConv.Records)))
		case netflow.OptionsDataFlowSet:
			NetFlowSetStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": versionStr,
					"type":    "OptionsDataFlowSet",
				}).
				Inc()

			NetFlowSetRecordsStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": versionStr,
					"type":    "OptionsDataFlowSet",
				}).
				Add(float64(len(fsConv.Records)))
		case netflow.DataFlowSet:
			NetFlowSetStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": versionStr,
					"type":    "DataFlowSet",
				}).
				Inc()

			NetFlowSetRecordsStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": versionStr,
					"type":    "DataFlowSet",
				}).
				Add(float64(len(fsConv.Records)))
		}
	}
}

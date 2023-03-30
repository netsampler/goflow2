package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	NAMESPACE = "goflow2"
)

var (
	MetricTrafficBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_traffic_bytes",
			Help:      "Bytes received by the application.",
			Namespace: NAMESPACE,
		},
		[]string{"remote_ip", "local_ip", "local_port", "type"},
	)
	MetricTrafficPackets = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_traffic_packets",
			Help:      "Packets received by the application.",
			Namespace: NAMESPACE},
		[]string{"remote_ip", "local_ip", "local_port", "type"},
	)
	MetricPacketSizeSum = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:      "flow_traffic_summary_size_bytes",
			Help:      "Summary of packet size.",
			Namespace: NAMESPACE, Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"remote_ip", "local_ip", "local_port", "type"},
	)
	DecoderStats = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_decoder_count",
			Help:      "Decoder processed count.",
			Namespace: NAMESPACE},
		[]string{"worker", "name"},
	)
	DecoderErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_decoder_error_count",
			Help:      "Decoder processed error count.",
			Namespace: NAMESPACE},
		[]string{"worker", "name"},
	)
	DecoderTime = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:      "flow_summary_decoding_time_us",
			Help:      "Decoding time summary.",
			Namespace: NAMESPACE, Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"name"},
	)
	DecoderProcessTime = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:      "flow_summary_processing_time_us",
			Help:      "Processing time summary.",
			Namespace: NAMESPACE, Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"name"},
	)
	NetFlowStats = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_nf_count",
			Help:      "NetFlows processed.",
			Namespace: NAMESPACE},
		[]string{"router", "version"},
	)
	NetFlowErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_nf_errors_count",
			Help:      "NetFlows processed errors.",
			Namespace: NAMESPACE},
		[]string{"router", "error"},
	)
	NetFlowSetRecordsStatsSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_nf_flowset_records_sum",
			Help:      "NetFlows FlowSets sum of records.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "type"}, // data-template, data, opts...
	)
	NetFlowSetStatsSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_nf_flowset_sum",
			Help:      "NetFlows FlowSets sum.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "type"}, // data-template, data, opts...
	)
	NetFlowTimeStatsSum = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:      "flow_process_nf_delay_summary_seconds",
			Help:      "NetFlows time difference between time of flow and processing.",
			Namespace: NAMESPACE, Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"router", "version"},
	)
	NetFlowTemplatesStats = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_nf_templates_count",
			Help:      "NetFlows Template count.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "obs_domain_id", "template_id", "type"}, // options/template
	)
	SFlowStats = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_sf_count",
			Help:      "sFlows processed.",
			Namespace: NAMESPACE},
		[]string{"router", "agent", "version"},
	)
	SFlowErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_sf_errors_count",
			Help:      "sFlows processed errors.",
			Namespace: NAMESPACE},
		[]string{"router", "error"},
	)
	SFlowSampleStatsSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_sf_samples_sum",
			Help:      "SFlows samples sum.",
			Namespace: NAMESPACE},
		[]string{"router", "agent", "version", "type"}, // counter, flow, expanded...
	)
	SFlowSampleRecordsStatsSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_sf_samples_records_sum",
			Help:      "SFlows samples sum of records.",
			Namespace: NAMESPACE},
		[]string{"router", "agent", "version", "type"}, // data-template, data, opts...
	)
)

func init() {
	prometheus.MustRegister(MetricTrafficBytes)
	prometheus.MustRegister(MetricTrafficPackets)
	prometheus.MustRegister(MetricPacketSizeSum)

	prometheus.MustRegister(DecoderStats)
	prometheus.MustRegister(DecoderErrors)
	prometheus.MustRegister(DecoderTime)
	prometheus.MustRegister(DecoderProcessTime)

	prometheus.MustRegister(NetFlowStats)
	prometheus.MustRegister(NetFlowErrors)
	prometheus.MustRegister(NetFlowSetRecordsStatsSum)
	prometheus.MustRegister(NetFlowSetStatsSum)
	prometheus.MustRegister(NetFlowTimeStatsSum)
	prometheus.MustRegister(NetFlowTemplatesStats)

	prometheus.MustRegister(SFlowStats)
	prometheus.MustRegister(SFlowErrors)
	prometheus.MustRegister(SFlowSampleStatsSum)
	prometheus.MustRegister(SFlowSampleRecordsStatsSum)
}

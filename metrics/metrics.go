// Package metrics exposes Prometheus collectors for flow processing.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// NAMESPACE is the Prometheus namespace prefix for goflow2 metrics.
	NAMESPACE = "goflow2"
)

var (
	// MetricReceivedDroppedPackets counts packets dropped before processing.
	MetricReceivedDroppedPackets = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_dropped_packets_total",
			Help:      "Packets dropped before processing.",
			Namespace: NAMESPACE,
		},
		[]string{"remote_ip", "local_ip", "local_port"},
	)
	// MetricReceivedDroppedBytes counts bytes dropped before processing.
	MetricReceivedDroppedBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_dropped_bytes_total",
			Help:      "Bytes dropped before processing.",
			Namespace: NAMESPACE,
		},
		[]string{"remote_ip", "local_ip", "local_port"},
	)
	// MetricTrafficBytes counts bytes received by the application.
	MetricTrafficBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_traffic_bytes_total",
			Help:      "Bytes received by the application.",
			Namespace: NAMESPACE,
		},
		[]string{"remote_ip", "local_ip", "local_port", "type"},
	)
	// MetricTrafficPackets counts packets received by the application.
	MetricTrafficPackets = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_traffic_packets_total",
			Help:      "Packets received by the application.",
			Namespace: NAMESPACE},
		[]string{"remote_ip", "local_ip", "local_port", "type"},
	)
	// MetricPacketSizeSum summarizes packet sizes.
	MetricPacketSizeSum = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:      "flow_traffic_size_bytes",
			Help:      "Summary of packet size.",
			Namespace: NAMESPACE, Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"remote_ip", "local_ip", "local_port", "type"},
	)
	// DecoderErrors counts decoder errors by router and name.
	DecoderErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_decoder_error_total",
			Help:      "NetFlow/sFlow processed errors.",
			Namespace: NAMESPACE},
		[]string{"router", "name"},
	)
	// DecoderTime summarizes decoding time.
	DecoderTime = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:      "flow_decoding_time_seconds",
			Help:      "Decoding time summary.",
			Namespace: NAMESPACE, Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"name"},
	)
	// NetFlowStats counts processed NetFlow packets by router and version.
	NetFlowStats = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_nf_total",
			Help:      "NetFlows processed.",
			Namespace: NAMESPACE},
		[]string{"router", "version"},
	)
	// NetFlowErrors counts NetFlow processing errors.
	NetFlowErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_nf_errors_total",
			Help:      "NetFlows processed errors.",
			Namespace: NAMESPACE},
		[]string{"router", "error"},
	)
	// NetFlowSetRecordsStatsSum counts flow set records by type.
	NetFlowSetRecordsStatsSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_nf_flowset_records_total",
			Help:      "NetFlows FlowSets sum of records.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "type"}, // data-template, data, opts...
	)
	// NetFlowSetStatsSum counts flow sets by type.
	NetFlowSetStatsSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_nf_flowset_total",
			Help:      "NetFlows FlowSets sum.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "type"}, // data-template, data, opts...
	)
	// NetFlowTimeStatsSum summarizes flow-to-processing latency.
	NetFlowTimeStatsSum = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:      "flow_process_nf_delay_seconds",
			Help:      "NetFlows time difference between time of flow and processing.",
			Namespace: NAMESPACE, Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"router", "version"},
	)
	// NetFlowTemplatesStats counts templates observed by router and version.
	NetFlowTemplatesStats = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_nf_templates_total",
			Help:      "NetFlows Template count.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "obs_domain_id", "template_id", "type"}, // options/template
	)
	// NetFlowTemplateAddedTimestamp records when a template was added.
	NetFlowTemplateAddedTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "flow_process_nf_template_added_timestamp_seconds",
			Help:      "Unix timestamp when a template was added.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "obs_domain_id", "template_id", "type"},
	)
	// NetFlowTemplateUpdatedTimestamp records when a template was updated.
	NetFlowTemplateUpdatedTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "flow_process_nf_template_updated_timestamp_seconds",
			Help:      "Unix timestamp when a template was updated.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "obs_domain_id", "template_id", "type"},
	)
	// NetFlowTemplateAccessedTimestamp records when a template was last accessed.
	NetFlowTemplateAccessedTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "flow_process_nf_template_accessed_timestamp_seconds",
			Help:      "Unix timestamp when a template was last accessed.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "obs_domain_id", "template_id", "type"},
	)
	// NetFlowTemplateEntries records currently live template entries.
	NetFlowTemplateEntries = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "flow_process_nf_template_entries",
			Help:      "Current NetFlow/IPFIX template entries.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "obs_domain_id", "template_id", "type"},
	)
	// SamplingRateEntries records currently live sampling-rate entries.
	SamplingRateEntries = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "flow_sampling_rate_entries",
			Help:      "Current sampling-rate entries.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "obs_domain_id"},
	)
	// SamplingRateUpdatedTimestamp records when a sampling rate was set or updated.
	SamplingRateUpdatedTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "flow_sampling_rate_updated_timestamp_seconds",
			Help:      "Unix timestamp when a sampling rate was set or updated.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "obs_domain_id"},
	)
	// SamplingRateAccessedTimestamp records when a sampling rate was last accessed.
	SamplingRateAccessedTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "flow_sampling_rate_accessed_timestamp_seconds",
			Help:      "Unix timestamp when a sampling rate was last accessed.",
			Namespace: NAMESPACE},
		[]string{"router", "version", "obs_domain_id"},
	)
	// SFlowStats counts processed sFlow packets.
	SFlowStats = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_sf_total",
			Help:      "sFlows processed.",
			Namespace: NAMESPACE},
		[]string{"router", "agent", "version"},
	)
	// SFlowSampleStatsSum counts sFlow samples by type.
	SFlowSampleStatsSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_sf_samples_total",
			Help:      "SFlows samples sum.",
			Namespace: NAMESPACE},
		[]string{"router", "agent", "version", "type"}, // counter, flow, expanded...
	)
	// SFlowSampleRecordsStatsSum counts sFlow sample records by type.
	SFlowSampleRecordsStatsSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "flow_process_sf_samples_records_total",
			Help:      "SFlows samples sum of records.",
			Namespace: NAMESPACE},
		[]string{"router", "agent", "version", "type"}, // data-template, data, opts...
	)
)

func init() {
	prometheus.MustRegister(MetricReceivedDroppedPackets)
	prometheus.MustRegister(MetricReceivedDroppedBytes)

	prometheus.MustRegister(MetricTrafficBytes)
	prometheus.MustRegister(MetricTrafficPackets)
	prometheus.MustRegister(MetricPacketSizeSum)

	prometheus.MustRegister(DecoderErrors)
	prometheus.MustRegister(DecoderTime)

	prometheus.MustRegister(NetFlowStats)
	prometheus.MustRegister(NetFlowErrors)
	prometheus.MustRegister(NetFlowSetRecordsStatsSum)
	prometheus.MustRegister(NetFlowSetStatsSum)
	prometheus.MustRegister(NetFlowTimeStatsSum)
	prometheus.MustRegister(NetFlowTemplatesStats)
	prometheus.MustRegister(NetFlowTemplateAddedTimestamp)
	prometheus.MustRegister(NetFlowTemplateUpdatedTimestamp)
	prometheus.MustRegister(NetFlowTemplateAccessedTimestamp)
	prometheus.MustRegister(NetFlowTemplateEntries)
	prometheus.MustRegister(SamplingRateEntries)
	prometheus.MustRegister(SamplingRateUpdatedTimestamp)
	prometheus.MustRegister(SamplingRateAccessedTimestamp)

	prometheus.MustRegister(SFlowStats)
	prometheus.MustRegister(SFlowSampleStatsSum)
	prometheus.MustRegister(SFlowSampleRecordsStatsSum)
}

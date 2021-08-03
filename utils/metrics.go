package utils

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	MetricTrafficBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_traffic_bytes",
			Help: "Bytes received by the application.",
		},
		[]string{"remote_ip", "local_ip", "local_port", "type"},
	)
	MetricTrafficPackets = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_traffic_packets",
			Help: "Packets received by the application.",
		},
		[]string{"remote_ip", "local_ip", "local_port", "type"},
	)
	MetricPacketSizeSum = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "flow_traffic_summary_size_bytes",
			Help:       "Summary of packet size.",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"remote_ip", "local_ip", "local_port", "type"},
	)
	DecoderStats = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_decoder_count",
			Help: "Decoder processed count.",
		},
		[]string{"worker", "name"},
	)
	DecoderErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_decoder_error_count",
			Help: "Decoder processed error count.",
		},
		[]string{"worker", "name"},
	)
	DecoderTime = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "flow_summary_decoding_time_us",
			Help:       "Decoding time summary.",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"name"},
	)
	DecoderProcessTime = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "flow_summary_processing_time_us",
			Help:       "Processing time summary.",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"name"},
	)
	NetFlowStats = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_process_nf_count",
			Help: "NetFlows processed.",
		},
		[]string{"router", "version"},
	)
	NetFlowErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_process_nf_errors_count",
			Help: "NetFlows processed errors.",
		},
		[]string{"router", "error"},
	)
	NetFlowSetRecordsStatsSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_process_nf_flowset_records_sum",
			Help: "NetFlows FlowSets sum of records.",
		},
		[]string{"router", "version", "type"}, // data-template, data, opts...
	)
	NetFlowSetStatsSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_process_nf_flowset_sum",
			Help: "NetFlows FlowSets sum.",
		},
		[]string{"router", "version", "type"}, // data-template, data, opts...
	)
	NetFlowTimeStatsSum = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "flow_process_nf_delay_summary_seconds",
			Help:       "NetFlows time difference between time of flow and processing.",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"router", "version"},
	)
	NetFlowTemplatesStats = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_process_nf_templates_count",
			Help: "NetFlows Template count.",
		},
		[]string{"router", "version", "obs_domain_id", "template_id", "type"}, // options/template
	)
	SFlowStats = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_process_sf_count",
			Help: "sFlows processed.",
		},
		[]string{"router", "agent", "version"},
	)
	SFlowErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_process_sf_errors_count",
			Help: "sFlows processed errors.",
		},
		[]string{"router", "error"},
	)
	SFlowSampleStatsSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_process_sf_samples_sum",
			Help: "SFlows samples sum.",
		},
		[]string{"router", "agent", "version", "type"}, // counter, flow, expanded...
	)
	SFlowSampleRecordsStatsSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "flow_process_sf_samples_records_sum",
			Help: "SFlows samples sum of records.",
		},
		[]string{"router", "agent", "version", "type"}, // data-template, data, opts...
	)
	SFlowIfSpeed = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifinspeed",
			Help: "SFlows IfInSpeed.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfDirection = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifdirection",
			Help: "SFlows IfDirection. 0 = unkown, 1=full-duplex, 2=half-duplex, 3 = in, 4=out.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfAdminStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifadminstatus",
			Help: "SFlows IfAdminStatus.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfOperStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifoperstatus",
			Help: "SFlows IfOperStatus.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfInOctets = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifinoctets",
			Help: "SFlows IfInOctets.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfInUcastPkts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifinucastpkts",
			Help: "SFlows IfInUcastPkts.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfInMulticastPkts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifinmulticastpkts",
			Help: "SFlows IfInMulticastPkts.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfInBroadcastPkts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifinbroadcastpkts",
			Help: "SFlows IfInBroadcastPkts.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfInDiscards = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifindiscards",
			Help: "SFlows IfInDiscards.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfInErrors = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifinerrors",
			Help: "SFlows IfInErrors.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfInUnknownProtos = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifinunknownprotos",
			Help: "SFlows IfInUnknownProtos.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfOutOctets = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifoutoctets",
			Help: "SFlows IfOutOctets.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfOutUcastPkts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifoutucastpkts",
			Help: "SFlows IfOutUcastPkts.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfOutMulticastPkts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifoutmulticastpkts",
			Help: "SFlows IfOutMulticastPkts.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfOutBroadcastPkts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifoutbroadcastpkts",
			Help: "SFlows IfOutBroadcastPkts.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfOutDiscards = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifoutdiscards",
			Help: "SFlows IfOutDiscards.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
	)
	SFlowIfOutErrors = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "flow_ifouterrors",
			Help: "SFlows IfOutErrors.",
		},
		[]string{"router", "agent", "version", "type", "ifindex", "iftype"},
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

	prometheus.MustRegister(SFlowIfSpeed)
	prometheus.MustRegister(SFlowIfDirection)
	prometheus.MustRegister(SFlowIfAdminStatus)
	prometheus.MustRegister(SFlowIfOperStatus)
	prometheus.MustRegister(SFlowIfInOctets)
	prometheus.MustRegister(SFlowIfInUcastPkts)
	prometheus.MustRegister(SFlowIfInMulticastPkts)
	prometheus.MustRegister(SFlowIfInBroadcastPkts)
	prometheus.MustRegister(SFlowIfInDiscards)
	prometheus.MustRegister(SFlowIfInErrors)
	prometheus.MustRegister(SFlowIfInUnknownProtos)
	prometheus.MustRegister(SFlowIfOutOctets)
	prometheus.MustRegister(SFlowIfOutUcastPkts)
	prometheus.MustRegister(SFlowIfOutMulticastPkts)
	prometheus.MustRegister(SFlowIfOutBroadcastPkts)
	prometheus.MustRegister(SFlowIfOutDiscards)
	prometheus.MustRegister(SFlowIfOutErrors)
}

func DefaultAccountCallback(name string, id int, start, end time.Time) {
	DecoderProcessTime.With(
		prometheus.Labels{
			"name": name,
		}).
		Observe(float64((end.Sub(start)).Nanoseconds()) / 1000)
	DecoderStats.With(
		prometheus.Labels{
			"worker": strconv.Itoa(id),
			"name":   name,
		}).
		Inc()
}

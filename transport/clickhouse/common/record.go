package common

const (
	FlowType            = "type"
	FlowTimeReceivedNs  = "time_received_ns"
	FlowSequenceNum     = "sequence_num"
	FlowSamplingRate    = "sampling_rate"
	FlowSrcAddr         = "src_addr"
	FlowDstAddr         = "dst_addr"
	FlowDstPort         = "dst_port"
	FlowBGPNextHop      = "bgp_next_hop"
	FlowProto           = "proto"
	FlowSamplerAddress  = "sampler_address"
	FlowTimeFlowStartNs = "time_flow_start_ns"
	FlowTimeFlowEndNs   = "time_flow_end_ns"
	FlowBytes           = "bytes"
	FlowPackets         = "packets"
	FlowDirection       = "flow_direction"
)

type EnrichmentRecord interface {
	Set(key string, value any)
	Get(key string) any
}

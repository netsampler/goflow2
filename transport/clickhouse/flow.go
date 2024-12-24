package clickhouse

type Flow struct {
	Type           int32  `ch:"type"`
	TimeReceivedNs uint64 `ch:"time_received_ns"`
	SequenceNum    uint32 `ch:"sequence_num"`
	SamplingRate   uint64 `ch:"sampling_rate"`

	SamplerAddress string `ch:"sampler_address"`

	TimeFlowStartNs uint64 `ch:"time_flow_start_ns"`
	TimeFlowEndNs   uint64 `ch:"time_flow_end_ns"`

	Bytes   uint64 `ch:"bytes"`
	Packets uint64 `ch:"packets"`

	SrcAddr string `ch:"src_addr"`
	DstAddr string `ch:"dst_addr"`

	Etype uint32 `ch:"etype"`

	Proto uint32 `ch:"proto"`

	SrcPort uint32 `ch:"src_port"`
	DstPort uint32 `ch:"dst_port"`

	ForwardingStatus uint32 `ch:"forwarding_status"`
	TcpFlags         uint32 `ch:"tcp_flags"`
	IcmpType         uint32 `ch:"icmp_type"`
	IcmpCode         uint32 `ch:"icmp_code"`

	FragmentId     uint32 `ch:"fragment_id"`
	FragmentOffset uint32 `ch:"fragment_offset"`
}

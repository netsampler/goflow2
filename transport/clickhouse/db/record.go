package db

import (
	"net/netip"
	"sync"

	flowpb "github.com/netsampler/goflow2/v2/pb"
	"github.com/netsampler/goflow2/v2/transport/clickhouse/common"
	"golang.org/x/exp/maps"
)

type DatabaseRecord struct {
	common.EnrichmentRecord

	kv map[string]any
}

var recordsPool = sync.Pool{
	New: func() any {
		return new(DatabaseRecord)
	},
}

func NewDbRecord() *DatabaseRecord {
	return &DatabaseRecord{kv: make(map[string]any)}
}

func RecordFromFlowMessage(flow *flowpb.FlowMessage) *DatabaseRecord {
	record := recordsPool.Get().(*DatabaseRecord)
	maps.Clear(record.kv)

	record.kv = map[string]any{
		common.FlowType:            flow.Type,
		common.FlowTimeReceivedNs:  flow.TimeReceivedNs,
		common.FlowSequenceNum:     flow.SequenceNum,
		common.FlowSamplingRate:    flow.SamplingRate,
		common.FlowSamplerAddress:  AddrFromSlice(flow.SamplerAddress),
		common.FlowTimeFlowStartNs: flow.TimeFlowStartNs,
		common.FlowTimeFlowEndNs:   flow.TimeFlowEndNs,
		common.FlowBytes:           flow.Bytes,
		common.FlowPackets:         flow.Packets,
		common.FlowSrcAddr:         AddrFromSlice(flow.SrcAddr),
		common.FlowDstAddr:         AddrFromSlice(flow.DstAddr),
		// common.FlowDirection:       flow.FlowDirection,
		"etype":                 flow.Etype,
		"proto":                 flow.Proto,
		"src_port":              flow.SrcPort,
		"dst_port":              flow.DstPort,
		"in_if":                 flow.InIf,
		"out_if":                flow.OutIf,
		"src_mac":               flow.SrcMac,
		"dst_mac":               flow.DstMac,
		"src_vlan":              flow.SrcVlan,
		"dst_vlan":              flow.DstVlan,
		"vlan_id":               flow.VlanId,
		"ip_tos":                flow.IpTos,
		"forwarding_status":     flow.ForwardingStatus,
		"ip_ttl":                flow.IpTtl,
		"ip_flags":              flow.IpFlags,
		"tcp_flags":             flow.TcpFlags,
		"icmp_type":             flow.IcmpType,
		"icmp_code":             flow.IcmpCode,
		"ipv6_flow_label":       flow.Ipv6FlowLabel,
		"fragment_id":           flow.FragmentId,
		"fragment_offset":       flow.FragmentOffset,
		"src_as":                flow.SrcAs,
		"dst_as":                flow.DstAs,
		"next_hop":              AddrFromSlice(flow.NextHop),
		"next_hop_as":           flow.NextHopAs,
		"src_net":               flow.SrcNet,
		"dst_net":               flow.DstNet,
		"bgp_next_hop":          AddrFromSlice(flow.BgpNextHop),
		"bgp_communities":       flow.BgpCommunities,
		"as_path":               flow.AsPath,
		"mpls_ttl":              flow.MplsTtl,
		"mpls_label":            flow.MplsLabel,
		"mpls_ip":               AddresesFromSlice(flow.MplsIp),
		"observation_domain_id": flow.ObservationDomainId,
		"observation_point_id":  flow.ObservationPointId,
	}
	return record
}

func AddrFromSlice(addr []byte) string {
	if ip, ok := netip.AddrFromSlice(addr); ok {
		return ip.String()
	} else {
		return ""
	}
}

func AddresesFromSlice(addresses [][]byte) []string {
	response := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		if ip, ok := netip.AddrFromSlice(addr); ok {
			response = append(response, ip.String())
		} else {
			//TODO: some err log here
		}
	}
	return response
}

func (d *DatabaseRecord) Set(key string, value any) {
	d.kv[key] = value
}

func (d *DatabaseRecord) Get(key string) any {
	return d.kv[key]
}

func (d *DatabaseRecord) Fields() []string {
	keys := make([]string, 0, len(d.kv))
	for k := range d.kv {
		keys = append(keys, k)
	}
	return keys
}

func (d *DatabaseRecord) Values(fields []string) []any {
	response := make([]any, 0, len(fields))
	for _, k := range fields {
		response = append(response, d.kv[k])
	}
	return response
}

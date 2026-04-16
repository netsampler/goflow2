package event

import "net/netip"

type PacketModel struct {
	Layers   []LayerSpec             `json:"layers,omitempty"`
	Features map[string]FeatureValue `json:"features,omitempty"`
}

type LayerSpec struct {
	Kind     string                  `json:"kind"`
	Name     string                  `json:"name,omitempty"`
	Ethernet *EthernetLayer          `json:"ethernet,omitempty"`
	VLAN     *VLANLayer              `json:"vlan,omitempty"`
	MPLS     *MPLSLayer              `json:"mpls,omitempty"`
	IPv4     *IPv4Layer              `json:"ipv4,omitempty"`
	IPv6     *IPv6Layer              `json:"ipv6,omitempty"`
	GRE      *GRELayer               `json:"gre,omitempty"`
	VXLAN    *VXLANLayer             `json:"vxlan,omitempty"`
	Geneve   *GeneveLayer            `json:"geneve,omitempty"`
	PPPoE    *PPPoELayer             `json:"pppoe,omitempty"`
	TCP      *TCPLayer               `json:"tcp,omitempty"`
	UDP      *UDPLayer               `json:"udp,omitempty"`
	ICMP     *ICMPLayer              `json:"icmp,omitempty"`
	Payload  *PayloadLayer           `json:"payload,omitempty"`
	Features map[string]FeatureValue `json:"features,omitempty"`
	Tags     map[string]string       `json:"tags,omitempty"`
}

type EthernetLayer struct {
	SrcMAC    string `json:"src_mac,omitempty"`
	DstMAC    string `json:"dst_mac,omitempty"`
	EtherType uint32 `json:"ether_type,omitempty"`
}

type VLANLayer struct {
	ID   uint16 `json:"id,omitempty"`
	PCP  uint8  `json:"pcp,omitempty"`
	DEI  bool   `json:"dei,omitempty"`
	TPID uint16 `json:"tpid,omitempty"`
}

type MPLSLabel struct {
	Label uint32 `json:"label,omitempty"`
	TC    uint8  `json:"tc,omitempty"`
	BOS   bool   `json:"bos,omitempty"`
	TTL   uint8  `json:"ttl,omitempty"`
}

type MPLSLayer struct {
	Label MPLSLabel `json:"label"`
}

type IPv4Layer struct {
	SrcAddr        netip.Addr `json:"src_addr,omitempty"`
	DstAddr        netip.Addr `json:"dst_addr,omitempty"`
	Protocol       uint8      `json:"protocol,omitempty"`
	TTL            uint8      `json:"ttl,omitempty"`
	DSCP           uint8      `json:"dscp,omitempty"`
	ECN            uint8      `json:"ecn,omitempty"`
	Identification uint16     `json:"identification,omitempty"`
	Flags          uint8      `json:"flags,omitempty"`
	FragmentOffset uint16     `json:"fragment_offset,omitempty"`
}

type IPv6Layer struct {
	SrcAddr      netip.Addr `json:"src_addr,omitempty"`
	DstAddr      netip.Addr `json:"dst_addr,omitempty"`
	NextHeader   uint8      `json:"next_header,omitempty"`
	HopLimit     uint8      `json:"hop_limit,omitempty"`
	TrafficClass uint8      `json:"traffic_class,omitempty"`
	FlowLabel    uint32     `json:"flow_label,omitempty"`
}

type GRELayer struct {
	Protocol uint16 `json:"protocol,omitempty"`
	Checksum bool   `json:"checksum,omitempty"`
	Key      bool   `json:"key,omitempty"`
	Sequence bool   `json:"sequence,omitempty"`
}

type VXLANLayer struct {
	VNI uint32 `json:"vni,omitempty"`
}

type GeneveLayer struct {
	VNI      uint32                  `json:"vni,omitempty"`
	Protocol uint16                  `json:"protocol,omitempty"`
	Options  map[string]FeatureValue `json:"options,omitempty"`
}

type PPPoELayer struct {
	SessionID uint16 `json:"session_id,omitempty"`
	Protocol  uint16 `json:"protocol,omitempty"`
}

type TCPLayer struct {
	SrcPort uint16 `json:"src_port,omitempty"`
	DstPort uint16 `json:"dst_port,omitempty"`
	Flags   uint8  `json:"flags,omitempty"`
	Seq     uint32 `json:"seq,omitempty"`
	Ack     uint32 `json:"ack,omitempty"`
	Window  uint16 `json:"window,omitempty"`
}

type UDPLayer struct {
	SrcPort uint16 `json:"src_port,omitempty"`
	DstPort uint16 `json:"dst_port,omitempty"`
}

type ICMPLayer struct {
	Type uint8 `json:"type,omitempty"`
	Code uint8 `json:"code,omitempty"`
}

type PayloadLayer struct {
	Length  uint32 `json:"length,omitempty"`
	Pattern string `json:"pattern,omitempty"`
}

type FeatureValue struct {
	String *string                 `json:"string,omitempty"`
	Uint64 *uint64                 `json:"uint64,omitempty"`
	Int64  *int64                  `json:"int64,omitempty"`
	Bool   *bool                   `json:"bool,omitempty"`
	Bytes  []byte                  `json:"bytes,omitempty"`
	List   []FeatureValue          `json:"list,omitempty"`
	Map    map[string]FeatureValue `json:"map,omitempty"`
}

func FeatureString(v string) FeatureValue {
	return FeatureValue{String: &v}
}

func FeatureUint64(v uint64) FeatureValue {
	return FeatureValue{Uint64: &v}
}

func FeatureInt64(v int64) FeatureValue {
	return FeatureValue{Int64: &v}
}

func FeatureBool(v bool) FeatureValue {
	return FeatureValue{Bool: &v}
}

func FeatureBytes(v []byte) FeatureValue {
	return FeatureValue{Bytes: append([]byte(nil), v...)}
}

func FeatureList(v ...FeatureValue) FeatureValue {
	return FeatureValue{List: append([]FeatureValue(nil), v...)}
}

func FeatureMap(v map[string]FeatureValue) FeatureValue {
	if len(v) == 0 {
		return FeatureValue{}
	}
	out := make(map[string]FeatureValue, len(v))
	for k, item := range v {
		out[k] = item
	}
	return FeatureValue{Map: out}
}

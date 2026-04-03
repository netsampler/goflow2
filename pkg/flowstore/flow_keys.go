package flowstore

// FlowIPv4Addr represents an IPv4 address used in flow keys.
type FlowIPv4Addr [4]byte

// FlowIPv6Addr represents an IPv6 address used in flow keys.
type FlowIPv6Addr [16]byte

// Compile-time guard: both types are usable as map keys (comparable).
var (
	_ = map[FlowIPv4Addr]struct{}{FlowIPv4Addr{}: {}}
	_ = map[FlowIPv6Addr]struct{}{FlowIPv6Addr{}: {}}
	_ = map[FlowIPv4Key]struct{}{}
	_ = map[FlowIPv6Key]struct{}{}
)

// FlowIPv4Key identifies a flow with IPv4 source and destination.
type FlowIPv4Key struct {
	Src FlowIPv4Addr
	Dst FlowIPv4Addr
}

// FlowIPv6Key identifies a flow with IPv6 source and destination.
type FlowIPv6Key struct {
	Src FlowIPv6Addr
	Dst FlowIPv6Addr
}

package flowstore

import "github.com/netsampler/goflow2/v3/decoders/netflow"

// These interfaces are reserved for future export paths where FlowStore values
// may be converted back into NetFlow/IPFIX records.

// NetFlowRecorder converts a value into a NetFlow/IPFIX data record.
// Reserved for future FlowStore export support.
type NetFlowRecorder interface {
	ToNetFlowRecord() (netflow.DataRecord, error)
}

// NetFlowOptionsRecorder converts a value into a NetFlow/IPFIX options data record.
// Reserved for future FlowStore export support.
type NetFlowOptionsRecorder interface {
	ToNetFlowOptionsRecord() (netflow.OptionsDataRecord, error)
}

// NetFlowScopeIndicator marks a value as a scope field contributor.
// Reserved for future FlowStore export support.
type NetFlowScopeIndicator interface {
	IsScope() bool
}

// NetFlowOptionIndicator marks a value as an option field contributor.
// Reserved for future FlowStore export support.
type NetFlowOptionIndicator interface {
	IsOption() bool
}

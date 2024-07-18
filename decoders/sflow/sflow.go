package sflow

import (
	"bytes"
	"fmt"

	"github.com/netsampler/goflow2/v2/decoders/utils"
)

// Opaque sample_data types according to https://sflow.org/SFLOW-DATAGRAM5.txt
const (
	SAMPLE_FORMAT_FLOW             = 1
	SAMPLE_FORMAT_COUNTER          = 2
	SAMPLE_FORMAT_EXPANDED_FLOW    = 3
	SAMPLE_FORMAT_EXPANDED_COUNTER = 4
	SAMPLE_FORMAT_DROP             = 5
)

// Opaque flow_data types according to https://sflow.org/SFLOW-STRUCTS5.txt
const (
	FLOW_TYPE_RAW              = 1
	FLOW_TYPE_ETH              = 2
	FLOW_TYPE_IPV4             = 3
	FLOW_TYPE_IPV6             = 4
	FLOW_TYPE_EXT_SWITCH       = 1001
	FLOW_TYPE_EXT_ROUTER       = 1002
	FLOW_TYPE_EXT_GATEWAY      = 1003
	FLOW_TYPE_EXT_USER         = 1004
	FLOW_TYPE_EXT_URL          = 1005
	FLOW_TYPE_EXT_MPLS         = 1006
	FLOW_TYPE_EXT_NAT          = 1007
	FLOW_TYPE_EXT_MPLS_TUNNEL  = 1008
	FLOW_TYPE_EXT_MPLS_VC      = 1009
	FLOW_TYPE_EXT_MPLS_FEC     = 1010
	FLOW_TYPE_EXT_MPLS_LVP_FEC = 1011
	FLOW_TYPE_EXT_VLAN_TUNNEL  = 1012

	// According to https://sflow.org/sflow_drops.txt
	FLOW_TYPE_EGRESS_QUEUE = 1036
	FLOW_TYPE_EXT_ACL      = 1037
	FLOW_TYPE_EXT_FUNCTION = 1038
)

// Opaque counter_data types according to https://sflow.org/SFLOW-STRUCTS5.txt
const (
	COUNTER_TYPE_IF        = 1
	COUNTER_TYPE_ETH       = 2
	COUNTER_TYPE_TOKENRING = 3
	COUNTER_TYPE_VG        = 4
	COUNTER_TYPE_VLAN      = 5
	COUNTER_TYPE_CPU       = 1001
)

type DecoderError struct {
	Err error
}

func (e *DecoderError) Error() string {
	return fmt.Sprintf("sFlow %s", e.Err.Error())
}

func (e *DecoderError) Unwrap() error {
	return e.Err
}

type FlowError struct {
	Format uint32
	Seq    uint32
	Err    error
}

func (e *FlowError) Error() string {
	return fmt.Sprintf("[format:%d seq:%d] %s", e.Format, e.Seq, e.Err.Error())
}

func (e *FlowError) Unwrap() error {
	return e.Err
}

type RecordError struct {
	DataFormat uint32
	Err        error
}

func (e *RecordError) Error() string {
	return fmt.Sprintf("[data-format:%d] %s", e.DataFormat, e.Err.Error())
}

func (e *RecordError) Unwrap() error {
	return e.Err
}

func DecodeIP(payload *bytes.Buffer) (uint32, []byte, error) {
	var ipVersion uint32
	if err := utils.BinaryDecoder(payload, &ipVersion); err != nil {
		return 0, nil, fmt.Errorf("DecodeIP: [%w]", err)
	}
	var ip []byte
	if ipVersion == 1 {
		ip = make([]byte, 4)
	} else if ipVersion == 2 {
		ip = make([]byte, 16)
	} else {
		return ipVersion, ip, fmt.Errorf("DecodeIP: unknown IP version %d", ipVersion)
	}
	if payload.Len() >= len(ip) {
		if err := utils.BinaryDecoder(payload, ip); err != nil {
			return 0, nil, fmt.Errorf("DecodeIP: [%w]", err)
		}
	} else {
		return ipVersion, ip, fmt.Errorf("DecodeIP: truncated data (need %d, got %d)", len(ip), payload.Len())
	}
	return ipVersion, ip, nil
}

func DecodeCounterRecord(header *RecordHeader, payload *bytes.Buffer) (CounterRecord, error) {
	counterRecord := CounterRecord{
		Header: *header,
	}
	switch header.DataFormat {
	case COUNTER_TYPE_IF:
		var ifCounters IfCounters
		if err := utils.BinaryDecoder(payload,
			&ifCounters.IfIndex,
			&ifCounters.IfType,
			&ifCounters.IfSpeed,
			&ifCounters.IfDirection,
			&ifCounters.IfStatus,
			&ifCounters.IfInOctets,
			&ifCounters.IfInUcastPkts,
			&ifCounters.IfInMulticastPkts,
			&ifCounters.IfInBroadcastPkts,
			&ifCounters.IfInDiscards,
			&ifCounters.IfInErrors,
			&ifCounters.IfInUnknownProtos,
			&ifCounters.IfOutOctets,
			&ifCounters.IfOutUcastPkts,
			&ifCounters.IfOutMulticastPkts,
			&ifCounters.IfOutBroadcastPkts,
			&ifCounters.IfOutDiscards,
			&ifCounters.IfOutErrors,
			&ifCounters.IfPromiscuousMode,
		); err != nil {
			return counterRecord, &RecordError{header.DataFormat, err}
		}
		counterRecord.Data = ifCounters
	case COUNTER_TYPE_ETH:
		var ethernetCounters EthernetCounters
		if err := utils.BinaryDecoder(payload,
			&ethernetCounters.Dot3StatsAlignmentErrors,
			&ethernetCounters.Dot3StatsFCSErrors,
			&ethernetCounters.Dot3StatsSingleCollisionFrames,
			&ethernetCounters.Dot3StatsMultipleCollisionFrames,
			&ethernetCounters.Dot3StatsSQETestErrors,
			&ethernetCounters.Dot3StatsDeferredTransmissions,
			&ethernetCounters.Dot3StatsLateCollisions,
			&ethernetCounters.Dot3StatsExcessiveCollisions,
			&ethernetCounters.Dot3StatsInternalMacTransmitErrors,
			&ethernetCounters.Dot3StatsCarrierSenseErrors,
			&ethernetCounters.Dot3StatsFrameTooLongs,
			&ethernetCounters.Dot3StatsInternalMacReceiveErrors,
			&ethernetCounters.Dot3StatsSymbolErrors,
		); err != nil {
			return counterRecord, &RecordError{header.DataFormat, err}
		}
		counterRecord.Data = ethernetCounters
	default:
		var rawRecord RawRecord
		rawRecord.Data = payload.Bytes()
		counterRecord.Data = rawRecord
	}

	return counterRecord, nil
}

func DecodeFlowRecord(header *RecordHeader, payload *bytes.Buffer) (FlowRecord, error) {
	flowRecord := FlowRecord{
		Header: *header,
	}
	var err error
	switch header.DataFormat {
	case FLOW_TYPE_RAW:
		sampledHeader := SampledHeader{}
		if err := utils.BinaryDecoder(payload,
			&sampledHeader.Protocol,
			&sampledHeader.FrameLength,
			&sampledHeader.Stripped,
			&sampledHeader.OriginalLength,
		); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		sampledHeader.HeaderData = payload.Bytes()
		flowRecord.Data = sampledHeader
	case FLOW_TYPE_ETH:
		sampledEth := SampledEthernet{
			SrcMac: make([]byte, 6),
			DstMac: make([]byte, 6),
		}
		if err := utils.BinaryDecoder(payload,
			&sampledEth.Length,
			sampledEth.SrcMac,
			sampledEth.DstMac,
			&sampledEth.EthType,
		); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		flowRecord.Data = sampledEth
	case FLOW_TYPE_IPV4:
		sampledIP := SampledIPv4{
			SampledIPBase: SampledIPBase{
				SrcIP: make([]byte, 4),
				DstIP: make([]byte, 4),
			},
		}
		if err := utils.BinaryDecoder(payload,
			&sampledIP.SampledIPBase.Length,
			&sampledIP.SampledIPBase.Protocol,
			sampledIP.SampledIPBase.SrcIP,
			sampledIP.SampledIPBase.DstIP,
			&sampledIP.SampledIPBase.SrcPort,
			&sampledIP.SampledIPBase.DstPort,
			&sampledIP.SampledIPBase.TcpFlags,
			&sampledIP.Tos,
		); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		flowRecord.Data = sampledIP
	case FLOW_TYPE_IPV6:
		sampledIP := SampledIPv6{
			SampledIPBase: SampledIPBase{
				SrcIP: make([]byte, 16),
				DstIP: make([]byte, 16),
			},
		}
		if err := utils.BinaryDecoder(payload,
			&sampledIP.SampledIPBase.Length,
			&sampledIP.SampledIPBase.Protocol,
			sampledIP.SampledIPBase.SrcIP,
			sampledIP.SampledIPBase.DstIP,
			&sampledIP.SampledIPBase.SrcPort,
			&sampledIP.SampledIPBase.DstPort,
			&sampledIP.SampledIPBase.TcpFlags,
			&sampledIP.Priority,
		); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		flowRecord.Data = sampledIP
	case FLOW_TYPE_EXT_SWITCH:
		extendedSwitch := ExtendedSwitch{}
		err := utils.BinaryDecoder(payload, &extendedSwitch.SrcVlan, &extendedSwitch.SrcPriority, &extendedSwitch.DstVlan, &extendedSwitch.DstPriority)
		if err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		flowRecord.Data = extendedSwitch
	case FLOW_TYPE_EXT_ROUTER:
		extendedRouter := ExtendedRouter{}
		if extendedRouter.NextHopIPVersion, extendedRouter.NextHop, err = DecodeIP(payload); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		if err := utils.BinaryDecoder(payload,
			&extendedRouter.SrcMaskLen,
			&extendedRouter.DstMaskLen,
		); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		flowRecord.Data = extendedRouter
	case FLOW_TYPE_EXT_GATEWAY:
		extendedGateway := ExtendedGateway{}
		if extendedGateway.NextHopIPVersion, extendedGateway.NextHop, err = DecodeIP(payload); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		if err := utils.BinaryDecoder(payload,
			&extendedGateway.AS,
			&extendedGateway.SrcAS,
			&extendedGateway.SrcPeerAS,
			&extendedGateway.ASDestinations,
		); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		var asPath []uint32
		if extendedGateway.ASDestinations != 0 {
			if err := utils.BinaryDecoder(payload,
				&extendedGateway.ASPathType,
				&extendedGateway.ASPathLength,
			); err != nil {
				return flowRecord, &RecordError{header.DataFormat, err}
			}
			// protection for as-path length
			if extendedGateway.ASPathLength > 1000 {
				return flowRecord, &RecordError{header.DataFormat, fmt.Errorf("as-path length of %d seems quite large", extendedGateway.ASPathLength)}
			}
			if int(extendedGateway.ASPathLength) > payload.Len()-4 {
				return flowRecord, &RecordError{header.DataFormat, fmt.Errorf("invalid AS path length: %d", extendedGateway.ASPathLength)}
			}
			asPath = make([]uint32, extendedGateway.ASPathLength) // max size of 1000 for protection
			if len(asPath) > 0 {
				if err := utils.BinaryDecoder(payload, asPath); err != nil {
					return flowRecord, &RecordError{header.DataFormat, err}
				}
			}
		}
		extendedGateway.ASPath = asPath

		if err := utils.BinaryDecoder(payload,
			&extendedGateway.CommunitiesLength,
		); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		// protection for communities length
		if extendedGateway.CommunitiesLength > 1000 {
			return flowRecord, &RecordError{header.DataFormat, fmt.Errorf("communities length of %d seems quite large", extendedGateway.ASPathLength)}
		}
		if int(extendedGateway.CommunitiesLength) > payload.Len()-4 {
			return flowRecord, &RecordError{header.DataFormat, fmt.Errorf("invalid communities length: %d", extendedGateway.ASPathLength)}
		}
		communities := make([]uint32, extendedGateway.CommunitiesLength) // max size of 1000 for protection
		if len(communities) > 0 {
			if err := utils.BinaryDecoder(payload, communities); err != nil {
				return flowRecord, &RecordError{header.DataFormat, err}
			}
		}
		if err := utils.BinaryDecoder(payload, &extendedGateway.LocalPref); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		extendedGateway.Communities = communities

		flowRecord.Data = extendedGateway
	case FLOW_TYPE_EGRESS_QUEUE:
		var queue EgressQueue
		if err := utils.BinaryDecoder(payload, &queue.Queue); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		flowRecord.Data = queue
	case FLOW_TYPE_EXT_ACL:
		var acl ExtendedACL
		if err := utils.BinaryDecoder(payload, &acl.Number, &acl.Name, &acl.Direction); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		flowRecord.Data = acl
	case FLOW_TYPE_EXT_FUNCTION:
		var function ExtendedFunction
		if err := utils.BinaryDecoder(payload, &function.Symbol); err != nil {
			return flowRecord, &RecordError{header.DataFormat, err}
		}
		flowRecord.Data = function
	default:
		var rawRecord RawRecord
		rawRecord.Data = payload.Bytes()
		flowRecord.Data = rawRecord
	}
	return flowRecord, nil
}

func DecodeSample(header *SampleHeader, payload *bytes.Buffer) (interface{}, error) {
	format := header.Format
	var sample interface{}

	if err := utils.BinaryDecoder(payload,
		&header.SampleSequenceNumber,
	); err != nil {
		return sample, fmt.Errorf("header seq [%w]", err)
	}
	seq := header.SampleSequenceNumber
	switch format {
	case SAMPLE_FORMAT_FLOW, SAMPLE_FORMAT_COUNTER:
		// Interlaced data-source format
		var sourceId uint32
		if err := utils.BinaryDecoder(payload, &sourceId); err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("header source [%w]", err)}
		}
		header.SourceIdType = sourceId >> 24
		header.SourceIdValue = sourceId & 0x00ffffff
	case SAMPLE_FORMAT_EXPANDED_FLOW, SAMPLE_FORMAT_EXPANDED_COUNTER, SAMPLE_FORMAT_DROP:
		// Explicit data-source format
		if err := utils.BinaryDecoder(payload,
			&header.SourceIdType,
			&header.SourceIdValue,
		); err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("header source [%w]", err)}
		}
	default:
		return sample, &FlowError{format, seq, fmt.Errorf("unknown format %d", format)}
	}

	var recordsCount uint32
	var flowSample FlowSample
	var counterSample CounterSample
	var expandedFlowSample ExpandedFlowSample
	var dropSample DropSample

	switch format {
	case SAMPLE_FORMAT_FLOW:
		flowSample.Header = *header
		if err := utils.BinaryDecoder(payload,
			&flowSample.SamplingRate,
			&flowSample.SamplePool,
			&flowSample.Drops,
			&flowSample.Input,
			&flowSample.Output,
			&flowSample.FlowRecordsCount,
		); err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("raw [%w]", err)}
		}
		recordsCount = flowSample.FlowRecordsCount
		if recordsCount > 1000 { // protection against ddos
			return sample, &FlowError{format, seq, fmt.Errorf("too many flow records: %d", recordsCount)}
		}
		flowSample.Records = make([]FlowRecord, recordsCount) // max size of 1000 for protection
		sample = flowSample
	case SAMPLE_FORMAT_COUNTER, SAMPLE_FORMAT_EXPANDED_COUNTER:
		counterSample.Header = *header
		if err := utils.BinaryDecoder(payload, &counterSample.CounterRecordsCount); err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("eth [%w]", err)}
		}
		recordsCount = counterSample.CounterRecordsCount
		if recordsCount > 1000 { // protection against ddos
			return sample, &FlowError{format, seq, fmt.Errorf("too many flow records: %d", recordsCount)}
		}
		counterSample.Records = make([]CounterRecord, recordsCount) // max size of 1000 for protection
		sample = counterSample
	case SAMPLE_FORMAT_EXPANDED_FLOW:
		expandedFlowSample.Header = *header
		if err := utils.BinaryDecoder(payload,
			&expandedFlowSample.SamplingRate,
			&expandedFlowSample.SamplePool,
			&expandedFlowSample.Drops,
			&expandedFlowSample.InputIfFormat,
			&expandedFlowSample.InputIfValue,
			&expandedFlowSample.OutputIfFormat,
			&expandedFlowSample.OutputIfValue,
			&expandedFlowSample.FlowRecordsCount,
		); err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("IPv4 [%w]", err)}
		}
		recordsCount = expandedFlowSample.FlowRecordsCount
		expandedFlowSample.Records = make([]FlowRecord, recordsCount)
		sample = expandedFlowSample
	case SAMPLE_FORMAT_DROP:
		dropSample.Header = *header
		if err := utils.BinaryDecoder(payload,
			&dropSample.Drops,
			&dropSample.Input,
			&dropSample.Output,
			&dropSample.Reason,
			&dropSample.FlowRecordsCount,
		); err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("raw [%w]", err)}
		}
		recordsCount = dropSample.FlowRecordsCount
		if recordsCount > 1000 { // protection against ddos
			return sample, &FlowError{format, seq, fmt.Errorf("too many flow records: %d", recordsCount)}
		}
		dropSample.Records = make([]FlowRecord, recordsCount) // max size of 1000 for protection
		sample = dropSample
	}
	for i := 0; i < int(recordsCount) && payload.Len() >= 8; i++ {
		recordHeader := RecordHeader{}
		if err := utils.BinaryDecoder(payload,
			&recordHeader.DataFormat,
			&recordHeader.Length,
		); err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("record header [%w]", err)}
		}
		if int(recordHeader.Length) > payload.Len() {
			break
		}
		recordReader := bytes.NewBuffer(payload.Next(int(recordHeader.Length)))
		switch format {
		case SAMPLE_FORMAT_FLOW:
			record, err := DecodeFlowRecord(&recordHeader, recordReader)
			if err != nil {
				return sample, &FlowError{format, seq, fmt.Errorf("record [%w]", err)}
			}
			flowSample.Records[i] = record
		case SAMPLE_FORMAT_COUNTER, SAMPLE_FORMAT_EXPANDED_COUNTER:
			record, err := DecodeCounterRecord(&recordHeader, recordReader)
			if err != nil {
				return sample, &FlowError{format, seq, fmt.Errorf("counter [%w]", err)}
			}
			counterSample.Records[i] = record
		case SAMPLE_FORMAT_EXPANDED_FLOW:
			record, err := DecodeFlowRecord(&recordHeader, recordReader)
			if err != nil {
				return sample, &FlowError{format, seq, fmt.Errorf("record [%w]", err)}
			}
			expandedFlowSample.Records[i] = record
		case SAMPLE_FORMAT_DROP:
			record, err := DecodeFlowRecord(&recordHeader, recordReader)
			if err != nil {
				return sample, &FlowError{format, seq, fmt.Errorf("record [%w]", err)}
			}
			dropSample.Records[i] = record
		}
	}
	return sample, nil
}

func DecodeMessageVersion(payload *bytes.Buffer, packetV5 *Packet) error {
	var version uint32
	if err := utils.BinaryDecoder(payload, &version); err != nil {
		return &DecoderError{err}
	}
	packetV5.Version = version

	if version != 5 {
		return &DecoderError{fmt.Errorf("unknown version %d", version)}
	}
	return DecodeMessage(payload, packetV5)
}

func DecodeMessage(payload *bytes.Buffer, packetV5 *Packet) error {
	if err := utils.BinaryDecoder(payload, &packetV5.IPVersion); err != nil {
		return &DecoderError{err}
	}
	var ip []byte
	if packetV5.IPVersion == 1 {
		ip = make([]byte, 4)
		if err := utils.BinaryDecoder(payload, ip); err != nil {
			return &DecoderError{fmt.Errorf("IPv4 [%w]", err)}
		}
	} else if packetV5.IPVersion == 2 {
		ip = make([]byte, 16)
		if err := utils.BinaryDecoder(payload, ip); err != nil {
			return &DecoderError{fmt.Errorf("IPv6 [%w]", err)}
		}
	} else {
		return &DecoderError{fmt.Errorf("unknown IP version %d", packetV5.IPVersion)}
	}

	packetV5.AgentIP = ip
	if err := utils.BinaryDecoder(payload,
		&packetV5.SubAgentId,
		&packetV5.SequenceNumber,
		&packetV5.Uptime,
		&packetV5.SamplesCount,
	); err != nil {
		return &DecoderError{err}
	}
	if packetV5.SamplesCount > 1000 {
		return &DecoderError{fmt.Errorf("too many samples: %d", packetV5.SamplesCount)}
	}

	packetV5.Samples = make([]interface{}, int(packetV5.SamplesCount)) // max size of 1000 for protection
	for i := 0; i < int(packetV5.SamplesCount) && payload.Len() >= 8; i++ {
		header := SampleHeader{}
		if err := utils.BinaryDecoder(payload, &header.Format, &header.Length); err != nil {
			return &DecoderError{fmt.Errorf("header [%w]", err)}
		}
		if int(header.Length) > payload.Len() {
			break
		}
		sampleReader := bytes.NewBuffer(payload.Next(int(header.Length)))

		sample, err := DecodeSample(&header, sampleReader)
		if err != nil {
			return &DecoderError{fmt.Errorf("sample [%w]", err)}
		} else {
			packetV5.Samples[i] = sample
		}
	}

	return nil
}

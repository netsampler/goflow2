// Package sflow decodes sFlow v5 datagrams.
package sflow

import (
	"bytes"
	"fmt"
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

// DecoderError wraps an sFlow decode error.
type DecoderError struct {
	Err error
}

func (e *DecoderError) Error() string {
	return fmt.Sprintf("sFlow %s", e.Err.Error())
}

func (e *DecoderError) Unwrap() error {
	return e.Err
}

// FlowError annotates an error with the sFlow sample format and sequence.
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

// RecordError annotates an error with the record data format.
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

// DecodeIP reads an sFlow IP address with version from the payload. The
// returned address is a sub-slice of the payload, valid as long as the
// datagram buffer is, like SampledHeader.HeaderData.
func DecodeIP(payload *bytes.Buffer) (uint32, []byte, error) {
	r := newXDRReader(payload)
	ipVersion, ip, err := r.ip()
	r.commit(payload)
	if err != nil {
		return ipVersion, ip, fmt.Errorf("DecodeIP: %w", err)
	}
	return ipVersion, ip, nil
}

// ip reads an address type followed by the address bytes.
func (r *xdrReader) ip() (uint32, []byte, error) {
	ipVersion := r.u32()
	if r.err != nil {
		return 0, nil, r.err
	}
	var size int
	switch ipVersion {
	case 0:
		return ipVersion, nil, nil
	case 1:
		size = 4
	case 2:
		size = 16
	default:
		return ipVersion, nil, fmt.Errorf("unknown IP version %d", ipVersion)
	}
	if r.remaining() < size {
		return ipVersion, nil, fmt.Errorf("truncated data (need %d, got %d)", size, r.remaining())
	}
	return ipVersion, r.bytes(size), nil
}

// DecodeCounterRecord decodes a counter record based on its data format.
func DecodeCounterRecord(header *RecordHeader, payload *bytes.Buffer) (CounterRecord, error) {
	r := newXDRReader(payload)
	record, err := decodeCounterRecord(header, &r)
	r.commit(payload)
	return record, err
}

func decodeCounterRecord(header *RecordHeader, r *xdrReader) (CounterRecord, error) {
	counterRecord := CounterRecord{
		Header: *header,
	}
	switch header.DataFormat {
	case COUNTER_TYPE_IF:
		var c IfCounters
		c.IfIndex = r.u32()
		c.IfType = r.u32()
		c.IfSpeed = r.u64()
		c.IfDirection = r.u32()
		c.IfStatus = r.u32()
		c.IfInOctets = r.u64()
		c.IfInUcastPkts = r.u32()
		c.IfInMulticastPkts = r.u32()
		c.IfInBroadcastPkts = r.u32()
		c.IfInDiscards = r.u32()
		c.IfInErrors = r.u32()
		c.IfInUnknownProtos = r.u32()
		c.IfOutOctets = r.u64()
		c.IfOutUcastPkts = r.u32()
		c.IfOutMulticastPkts = r.u32()
		c.IfOutBroadcastPkts = r.u32()
		c.IfOutDiscards = r.u32()
		c.IfOutErrors = r.u32()
		c.IfPromiscuousMode = r.u32()
		if r.err != nil {
			return counterRecord, &RecordError{header.DataFormat, r.err}
		}
		counterRecord.Data = c
	case COUNTER_TYPE_ETH:
		var c EthernetCounters
		c.Dot3StatsAlignmentErrors = r.u32()
		c.Dot3StatsFCSErrors = r.u32()
		c.Dot3StatsSingleCollisionFrames = r.u32()
		c.Dot3StatsMultipleCollisionFrames = r.u32()
		c.Dot3StatsSQETestErrors = r.u32()
		c.Dot3StatsDeferredTransmissions = r.u32()
		c.Dot3StatsLateCollisions = r.u32()
		c.Dot3StatsExcessiveCollisions = r.u32()
		c.Dot3StatsInternalMacTransmitErrors = r.u32()
		c.Dot3StatsCarrierSenseErrors = r.u32()
		c.Dot3StatsFrameTooLongs = r.u32()
		c.Dot3StatsInternalMacReceiveErrors = r.u32()
		c.Dot3StatsSymbolErrors = r.u32()
		if r.err != nil {
			return counterRecord, &RecordError{header.DataFormat, r.err}
		}
		counterRecord.Data = c
	default:
		var rawRecord RawRecord
		rawRecord.Data = r.rest()
		counterRecord.Data = rawRecord
	}

	return counterRecord, nil
}

// DecodeFlowRecord decodes a flow record based on its data format.
//
// Byte-slice fields of the decoded records (MAC and IP addresses, header data,
// raw records) are sub-slices of the payload and stay valid only as long as
// the datagram buffer is not reused.
func DecodeFlowRecord(header *RecordHeader, payload *bytes.Buffer) (FlowRecord, error) {
	r := newXDRReader(payload)
	record, err := decodeFlowRecord(header, &r)
	r.commit(payload)
	return record, err
}

func decodeFlowRecord(header *RecordHeader, r *xdrReader) (FlowRecord, error) {
	flowRecord := FlowRecord{
		Header: *header,
	}
	switch header.DataFormat {
	case FLOW_TYPE_RAW:
		sampledHeader := SampledHeader{}
		sampledHeader.Protocol = r.u32()
		sampledHeader.FrameLength = r.u32()
		sampledHeader.Stripped = r.u32()
		headerLength := r.u32()
		sampledHeader.HeaderData = r.opaque(headerLength)
		if r.err != nil {
			return flowRecord, &RecordError{header.DataFormat, r.err}
		}
		sampledHeader.OriginalLength = headerLength
		flowRecord.Data = sampledHeader
	case FLOW_TYPE_ETH:
		// Per sFlow v5 (RFC 3176), MAC addresses are encoded as XDR opaque
		// fixed-length and padded to a multiple of 4 bytes. A 6-byte MAC is
		// transmitted as 8 bytes (6 bytes of address + 2 zero pad bytes).
		sampledEth := SampledEthernet{}
		sampledEth.Length = r.u32()
		sampledEth.SrcMac = r.bytes(6)
		r.skip(2)
		sampledEth.DstMac = r.bytes(6)
		r.skip(2)
		sampledEth.EthType = r.u32()
		if r.err != nil {
			return flowRecord, &RecordError{header.DataFormat, r.err}
		}
		flowRecord.Data = sampledEth
	case FLOW_TYPE_IPV4:
		sampledIP := SampledIPv4{}
		sampledIP.Length = r.u32()
		sampledIP.Protocol = r.u32()
		sampledIP.SrcIP = r.bytes(4)
		sampledIP.DstIP = r.bytes(4)
		sampledIP.SrcPort = r.u32()
		sampledIP.DstPort = r.u32()
		sampledIP.TcpFlags = r.u32()
		sampledIP.Tos = r.u32()
		if r.err != nil {
			return flowRecord, &RecordError{header.DataFormat, r.err}
		}
		flowRecord.Data = sampledIP
	case FLOW_TYPE_IPV6:
		sampledIP := SampledIPv6{}
		sampledIP.Length = r.u32()
		sampledIP.Protocol = r.u32()
		sampledIP.SrcIP = r.bytes(16)
		sampledIP.DstIP = r.bytes(16)
		sampledIP.SrcPort = r.u32()
		sampledIP.DstPort = r.u32()
		sampledIP.TcpFlags = r.u32()
		sampledIP.Priority = r.u32()
		if r.err != nil {
			return flowRecord, &RecordError{header.DataFormat, r.err}
		}
		flowRecord.Data = sampledIP
	case FLOW_TYPE_EXT_SWITCH:
		extendedSwitch := ExtendedSwitch{}
		extendedSwitch.SrcVlan = r.u32()
		extendedSwitch.SrcPriority = r.u32()
		extendedSwitch.DstVlan = r.u32()
		extendedSwitch.DstPriority = r.u32()
		if r.err != nil {
			return flowRecord, &RecordError{header.DataFormat, r.err}
		}
		flowRecord.Data = extendedSwitch
	case FLOW_TYPE_EXT_ROUTER:
		extendedRouter := ExtendedRouter{}
		var err error
		if extendedRouter.NextHopIPVersion, extendedRouter.NextHop, err = r.ip(); err != nil {
			return flowRecord, &RecordError{header.DataFormat, fmt.Errorf("DecodeIP: %w", err)}
		}
		extendedRouter.SrcMaskLen = r.u32()
		extendedRouter.DstMaskLen = r.u32()
		if r.err != nil {
			return flowRecord, &RecordError{header.DataFormat, r.err}
		}
		flowRecord.Data = extendedRouter
	case FLOW_TYPE_EXT_GATEWAY:
		extendedGateway := ExtendedGateway{}
		var err error
		if extendedGateway.NextHopIPVersion, extendedGateway.NextHop, err = r.ip(); err != nil {
			return flowRecord, &RecordError{header.DataFormat, fmt.Errorf("DecodeIP: %w", err)}
		}
		extendedGateway.AS = r.u32()
		extendedGateway.SrcAS = r.u32()
		extendedGateway.SrcPeerAS = r.u32()
		asPathCount := r.u32()
		if r.err != nil {
			return flowRecord, &RecordError{header.DataFormat, r.err}
		}
		if asPathCount > 1000 {
			return flowRecord, &RecordError{header.DataFormat, fmt.Errorf("as-path segments of %d seems quite large", asPathCount)}
		}
		asPathSegments := make([]ASPathSegment, 0, asPathCount)
		for i := 0; i < int(asPathCount); i++ {
			segmentType := r.u32()
			segmentLength := r.u32()
			if r.err != nil {
				return flowRecord, &RecordError{header.DataFormat, r.err}
			}
			if segmentLength > 1000 {
				return flowRecord, &RecordError{header.DataFormat, fmt.Errorf("as-path length of %d seems quite large", segmentLength)}
			}
			if int(segmentLength) > r.remaining()/4 {
				return flowRecord, &RecordError{header.DataFormat, fmt.Errorf("invalid AS path length: %d", segmentLength)}
			}
			segmentPath := make([]uint32, 0)
			if segmentLength > 0 {
				segmentPath = r.u32s(int(segmentLength))
				if r.err != nil {
					return flowRecord, &RecordError{header.DataFormat, r.err}
				}
			}
			asPathSegments = append(asPathSegments, ASPathSegment{
				Type: segmentType,
				Path: segmentPath,
			})
		}
		extendedGateway.ASDestinations = asPathCount
		extendedGateway.DstASPath = asPathSegments
		if asPathCount == 1 {
			extendedGateway.ASPathType = asPathSegments[0].Type
			extendedGateway.ASPathLength = uint32(len(asPathSegments[0].Path))
			extendedGateway.ASPath = asPathSegments[0].Path
		}

		extendedGateway.CommunitiesLength = r.u32()
		if r.err != nil {
			return flowRecord, &RecordError{header.DataFormat, r.err}
		}
		// protection for communities length
		if extendedGateway.CommunitiesLength > 1000 {
			return flowRecord, &RecordError{header.DataFormat, fmt.Errorf("communities length of %d seems quite large", extendedGateway.CommunitiesLength)}
		}
		if int(extendedGateway.CommunitiesLength) > r.remaining()/4 {
			return flowRecord, &RecordError{header.DataFormat, fmt.Errorf("invalid communities length: %d", extendedGateway.CommunitiesLength)}
		}
		communities := make([]uint32, 0)
		if extendedGateway.CommunitiesLength > 0 {
			communities = r.u32s(int(extendedGateway.CommunitiesLength))
		}
		extendedGateway.LocalPref = r.u32()
		if r.err != nil {
			return flowRecord, &RecordError{header.DataFormat, r.err}
		}
		extendedGateway.Communities = communities

		flowRecord.Data = extendedGateway
	case FLOW_TYPE_EGRESS_QUEUE:
		var queue EgressQueue
		queue.Queue = r.u32()
		if r.err != nil {
			return flowRecord, &RecordError{header.DataFormat, r.err}
		}
		flowRecord.Data = queue
	case FLOW_TYPE_EXT_ACL:
		var acl ExtendedACL
		acl.Number = r.u32()
		acl.Name = r.str()
		acl.Direction = r.u32()
		if r.err != nil {
			return flowRecord, &RecordError{header.DataFormat, r.err}
		}
		flowRecord.Data = acl
	case FLOW_TYPE_EXT_FUNCTION:
		var function ExtendedFunction
		function.Symbol = r.str()
		if r.err != nil {
			return flowRecord, &RecordError{header.DataFormat, r.err}
		}
		flowRecord.Data = function
	default:
		var rawRecord RawRecord
		rawRecord.Data = r.rest()
		flowRecord.Data = rawRecord
	}
	return flowRecord, nil
}

func DecodeSample(header *SampleHeader, payload *bytes.Buffer) (interface{}, error) {
	r := newXDRReader(payload)
	sample, err := decodeSample(header, &r)
	r.commit(payload)
	return sample, err
}

func decodeSample(header *SampleHeader, r *xdrReader) (interface{}, error) {
	format := header.Format
	var sample interface{}

	header.SampleSequenceNumber = r.u32()
	if r.err != nil {
		return sample, fmt.Errorf("header seq [%w]", r.err)
	}
	seq := header.SampleSequenceNumber
	switch format {
	case SAMPLE_FORMAT_FLOW, SAMPLE_FORMAT_COUNTER:
		// Interlaced data-source format
		sourceId := r.u32()
		if r.err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("header source [%w]", r.err)}
		}
		header.SourceIdType = sourceId >> 24
		header.SourceIdValue = sourceId & 0x00ffffff
	case SAMPLE_FORMAT_EXPANDED_FLOW, SAMPLE_FORMAT_EXPANDED_COUNTER, SAMPLE_FORMAT_DROP:
		// Explicit data-source format
		header.SourceIdType = r.u32()
		header.SourceIdValue = r.u32()
		if r.err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("header source [%w]", r.err)}
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
		flowSample.SamplingRate = r.u32()
		flowSample.SamplePool = r.u32()
		flowSample.Drops = r.u32()
		flowSample.Input = r.u32()
		flowSample.Output = r.u32()
		flowSample.FlowRecordsCount = r.u32()
		if r.err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("raw [%w]", r.err)}
		}
		recordsCount = flowSample.FlowRecordsCount
		if recordsCount > 1000 { // protection against ddos
			return sample, &FlowError{format, seq, fmt.Errorf("too many flow records: %d", recordsCount)}
		}
		flowSample.Records = make([]FlowRecord, recordsCount) // max size of 1000 for protection
		sample = flowSample
	case SAMPLE_FORMAT_COUNTER, SAMPLE_FORMAT_EXPANDED_COUNTER:
		counterSample.Header = *header
		counterSample.CounterRecordsCount = r.u32()
		if r.err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("eth [%w]", r.err)}
		}
		recordsCount = counterSample.CounterRecordsCount
		if recordsCount > 1000 { // protection against ddos
			return sample, &FlowError{format, seq, fmt.Errorf("too many flow records: %d", recordsCount)}
		}
		counterSample.Records = make([]CounterRecord, recordsCount) // max size of 1000 for protection
		sample = counterSample
	case SAMPLE_FORMAT_EXPANDED_FLOW:
		expandedFlowSample.Header = *header
		expandedFlowSample.SamplingRate = r.u32()
		expandedFlowSample.SamplePool = r.u32()
		expandedFlowSample.Drops = r.u32()
		expandedFlowSample.InputIfFormat = r.u32()
		expandedFlowSample.InputIfValue = r.u32()
		expandedFlowSample.OutputIfFormat = r.u32()
		expandedFlowSample.OutputIfValue = r.u32()
		expandedFlowSample.FlowRecordsCount = r.u32()
		if r.err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("IPv4 [%w]", r.err)}
		}
		recordsCount = expandedFlowSample.FlowRecordsCount
		if recordsCount > 1000 { // protection against ddos
			return sample, &FlowError{format, seq, fmt.Errorf("too many flow records: %d", recordsCount)}
		}
		expandedFlowSample.Records = make([]FlowRecord, recordsCount)
		sample = expandedFlowSample
	case SAMPLE_FORMAT_DROP:
		dropSample.Header = *header
		dropSample.Drops = r.u32()
		dropSample.Input = r.u32()
		dropSample.Output = r.u32()
		dropSample.Reason = r.u32()
		dropSample.FlowRecordsCount = r.u32()
		if r.err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("raw [%w]", r.err)}
		}
		recordsCount = dropSample.FlowRecordsCount
		if recordsCount > 1000 { // protection against ddos
			return sample, &FlowError{format, seq, fmt.Errorf("too many flow records: %d", recordsCount)}
		}
		dropSample.Records = make([]FlowRecord, recordsCount) // max size of 1000 for protection
		sample = dropSample
	}
	for i := 0; i < int(recordsCount) && r.remaining() >= 8; i++ {
		recordHeader := RecordHeader{}
		recordHeader.DataFormat = r.u32()
		recordHeader.Length = r.u32()
		if r.err != nil {
			return sample, &FlowError{format, seq, fmt.Errorf("record header [%w]", r.err)}
		}
		if int(recordHeader.Length) > r.remaining() {
			break
		}
		// A reader over this record only; it lives on the stack.
		recordReader := r.sub(int(recordHeader.Length))
		switch format {
		case SAMPLE_FORMAT_FLOW:
			record, err := decodeFlowRecord(&recordHeader, &recordReader)
			if err != nil {
				return sample, &FlowError{format, seq, fmt.Errorf("record [%w]", err)}
			}
			flowSample.Records[i] = record
		case SAMPLE_FORMAT_COUNTER, SAMPLE_FORMAT_EXPANDED_COUNTER:
			record, err := decodeCounterRecord(&recordHeader, &recordReader)
			if err != nil {
				return sample, &FlowError{format, seq, fmt.Errorf("counter [%w]", err)}
			}
			counterSample.Records[i] = record
		case SAMPLE_FORMAT_EXPANDED_FLOW:
			record, err := decodeFlowRecord(&recordHeader, &recordReader)
			if err != nil {
				return sample, &FlowError{format, seq, fmt.Errorf("record [%w]", err)}
			}
			expandedFlowSample.Records[i] = record
		case SAMPLE_FORMAT_DROP:
			record, err := decodeFlowRecord(&recordHeader, &recordReader)
			if err != nil {
				return sample, &FlowError{format, seq, fmt.Errorf("record [%w]", err)}
			}
			dropSample.Records[i] = record
		}
	}
	return sample, nil
}

func DecodeMessageVersion(payload *bytes.Buffer, packetV5 *Packet) error {
	r := newXDRReader(payload)
	version := r.u32()
	r.commit(payload)
	if r.err != nil {
		return &DecoderError{fmt.Errorf("version [%w]", r.err)}
	}
	packetV5.Version = version

	if version != 5 {
		return &DecoderError{fmt.Errorf("unknown version %d", version)}
	}
	return DecodeMessage(payload, packetV5)
}

// DecodeMessage decodes an sFlow v5 datagram body into packetV5.
//
// The decoded packet references the payload: AgentIP, addresses, MACs and
// header data are sub-slices of it. The caller must finish using the packet
// before reusing the datagram buffer.
func DecodeMessage(payload *bytes.Buffer, packetV5 *Packet) error {
	r := newXDRReader(payload)
	err := decodeMessage(&r, packetV5)
	r.commit(payload)
	return err
}

func decodeMessage(r *xdrReader, packetV5 *Packet) error {
	packetV5.IPVersion = r.u32()
	if r.err != nil {
		return &DecoderError{fmt.Errorf("IP version [%w]", r.err)}
	}
	var ip []byte
	switch packetV5.IPVersion {
	case 0:
		ip = nil
	case 1:
		ip = r.bytes(4)
		if r.err != nil {
			return &DecoderError{fmt.Errorf("IPv4 [%w]", r.err)}
		}
	case 2:
		ip = r.bytes(16)
		if r.err != nil {
			return &DecoderError{fmt.Errorf("IPv6 [%w]", r.err)}
		}
	default:
		return &DecoderError{fmt.Errorf("unknown IP version %d", packetV5.IPVersion)}
	}

	packetV5.AgentIP = ip
	packetV5.SubAgentId = r.u32()
	packetV5.SequenceNumber = r.u32()
	packetV5.Uptime = r.u32()
	packetV5.SamplesCount = r.u32()
	if r.err != nil {
		return &DecoderError{fmt.Errorf("header [%w]", r.err)}
	}
	if packetV5.SamplesCount > 1000 {
		return &DecoderError{fmt.Errorf("too many samples: %d", packetV5.SamplesCount)}
	}

	packetV5.Samples = make([]interface{}, int(packetV5.SamplesCount)) // max size of 1000 for protection
	for i := 0; i < int(packetV5.SamplesCount) && r.remaining() >= 8; i++ {
		header := SampleHeader{}
		header.Format = r.u32()
		header.Length = r.u32()
		if r.err != nil {
			return &DecoderError{fmt.Errorf("header [%w]", r.err)}
		}
		if int(header.Length) > r.remaining() {
			break
		}
		// A reader over this sample only; it lives on the stack.
		sampleReader := r.sub(int(header.Length))

		sample, err := decodeSample(&header, &sampleReader)
		if err != nil {
			return &DecoderError{fmt.Errorf("sample [%w]", err)}
		}
		packetV5.Samples[i] = sample
	}

	return nil
}

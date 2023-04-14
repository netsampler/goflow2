package sflow

import (
	"bytes"
	"fmt"

	"github.com/netsampler/goflow2/decoders/utils"
)

const (
	FORMAT_EXT_SWITCH  = 1001
	FORMAT_EXT_ROUTER  = 1002
	FORMAT_EXT_GATEWAY = 1003
	FORMAT_RAW_PKT     = 1
	FORMAT_ETH         = 2
	FORMAT_IPV4        = 3
	FORMAT_IPV6        = 4
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
	DataFormat uint32
	Err        error
}

func (e *FlowError) Error() string {
	return fmt.Sprintf("[data-format:%d] %s", e.DataFormat, e.Err.Error())
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

type ErrorDecodingSFlow struct {
	msg string
}

func NewErrorDecodingSFlow(msg string) *ErrorDecodingSFlow {
	return &ErrorDecodingSFlow{
		msg: msg,
	}
}

func (e *ErrorDecodingSFlow) Error() string {
	return fmt.Sprintf("Error decoding sFlow: %v", e.msg)
}

type ErrorDataFormat struct {
	dataformat uint32
}

func NewErrorDataFormat(dataformat uint32) *ErrorDataFormat {
	return &ErrorDataFormat{
		dataformat: dataformat,
	}
}

func (e *ErrorDataFormat) Error() string {
	return fmt.Sprintf("Unknown data format %v", e.dataformat)
}

func DecodeCounterRecord(header *RecordHeader, payload *bytes.Buffer) (CounterRecord, error) {
	counterRecord := CounterRecord{
		Header: *header,
	}
	switch (*header).DataFormat {
	case 1:
		var ifCounters IfCounters
		if err := utils.BinaryDecoder(payload, &ifCounters); err != nil {
			return counterRecord, &RecordError{(*header).DataFormat, err}
		}
		counterRecord.Data = ifCounters
	case 2:
		var ethernetCounters EthernetCounters
		if err := utils.BinaryDecoder(payload, &ethernetCounters); err != nil {
			return counterRecord, &RecordError{(*header).DataFormat, err}
		}
		counterRecord.Data = ethernetCounters
	default:
		return counterRecord, &RecordError{(*header).DataFormat, fmt.Errorf("unknown counter data format")}
	}

	return counterRecord, nil
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
		if err := utils.BinaryDecoder(payload, &ip); err != nil {
			return 0, nil, fmt.Errorf("DecodeIP: [%w]", err)
		}
	} else {
		return ipVersion, ip, fmt.Errorf("DecodeIP: truncated data (need %d, got %d)", len(ip), payload.Len())
	}
	return ipVersion, ip, nil
}

func DecodeFlowRecord(header *RecordHeader, payload *bytes.Buffer) (FlowRecord, error) {
	flowRecord := FlowRecord{
		Header: *header,
	}
	var err error
	switch (*header).DataFormat {
	case FORMAT_EXT_SWITCH:
		extendedSwitch := ExtendedSwitch{}
		err := utils.BinaryDecoder(payload, &extendedSwitch)
		if err != nil {
			return flowRecord, &RecordError{(*header).DataFormat, err}
		}
		flowRecord.Data = extendedSwitch
	case FORMAT_RAW_PKT:
		sampledHeader := SampledHeader{}
		if err := utils.BinaryDecoder(payload, &(sampledHeader.Protocol), &(sampledHeader.FrameLength), &(sampledHeader.Stripped), &(sampledHeader.OriginalLength)); err != nil {
			return flowRecord, &RecordError{(*header).DataFormat, err}
		}
		sampledHeader.HeaderData = payload.Bytes()
		flowRecord.Data = sampledHeader
	case FORMAT_IPV4:
		sampledIPBase := SampledIP_Base{
			SrcIP: make([]byte, 4),
			DstIP: make([]byte, 4),
		}
		if err := utils.BinaryDecoder(payload, &sampledIPBase); err != nil {
			return flowRecord, &RecordError{(*header).DataFormat, err}
		}
		sampledIPv4 := SampledIPv4{
			Base: sampledIPBase,
		}
		if err := utils.BinaryDecoder(payload, &(sampledIPv4.Tos)); err != nil {
			return flowRecord, &RecordError{(*header).DataFormat, err}
		}
		flowRecord.Data = sampledIPv4
	case FORMAT_IPV6:
		sampledIPBase := SampledIP_Base{
			SrcIP: make([]byte, 16),
			DstIP: make([]byte, 16),
		}
		if err := utils.BinaryDecoder(payload, &sampledIPBase); err != nil {
			return flowRecord, &RecordError{(*header).DataFormat, err}
		}
		sampledIPv6 := SampledIPv6{
			Base: sampledIPBase,
		}
		if err := utils.BinaryDecoder(payload, &(sampledIPv6.Priority)); err != nil {
			return flowRecord, &RecordError{(*header).DataFormat, err}
		}
		flowRecord.Data = sampledIPv6
	case FORMAT_EXT_ROUTER:
		extendedRouter := ExtendedRouter{}
		if extendedRouter.NextHopIPVersion, extendedRouter.NextHop, err = DecodeIP(payload); err != nil {
			return flowRecord, &RecordError{(*header).DataFormat, err}
		}
		if err := utils.BinaryDecoder(payload, &(extendedRouter.SrcMaskLen), &(extendedRouter.DstMaskLen)); err != nil {
			return flowRecord, &RecordError{(*header).DataFormat, err}
		}
		flowRecord.Data = extendedRouter
	case FORMAT_EXT_GATEWAY:
		extendedGateway := ExtendedGateway{}
		if extendedGateway.NextHopIPVersion, extendedGateway.NextHop, err = DecodeIP(payload); err != nil {
			return flowRecord, &RecordError{(*header).DataFormat, err}
		}
		if err := utils.BinaryDecoder(payload, &(extendedGateway.AS), &(extendedGateway.SrcAS), &(extendedGateway.SrcPeerAS),
			&(extendedGateway.ASDestinations)); err != nil {
			return flowRecord, &RecordError{(*header).DataFormat, err}
		}
		var asPath []uint32
		if extendedGateway.ASDestinations != 0 {
			if err := utils.BinaryDecoder(payload, &(extendedGateway.ASPathType), &(extendedGateway.ASPathLength)); err != nil {
				return flowRecord, &RecordError{(*header).DataFormat, err}
			}
			if int(extendedGateway.ASPathLength) > payload.Len()-4 {
				return flowRecord, &RecordError{(*header).DataFormat, fmt.Errorf("invalid AS path length: %d", extendedGateway.ASPathLength)}
			}
			asPath = make([]uint32, extendedGateway.ASPathLength)
			if len(asPath) > 0 {
				if err := utils.BinaryDecoder(payload, asPath); err != nil {
					return flowRecord, &RecordError{(*header).DataFormat, err}
				}
			}
		}
		extendedGateway.ASPath = asPath

		if err := utils.BinaryDecoder(payload, &(extendedGateway.CommunitiesLength)); err != nil {
			return flowRecord, &RecordError{(*header).DataFormat, err}
		}
		if int(extendedGateway.CommunitiesLength) > payload.Len()-4 {
			return flowRecord, &RecordError{(*header).DataFormat, fmt.Errorf("invalid Communities length: %d", extendedGateway.ASPathLength)}
		}
		communities := make([]uint32, extendedGateway.CommunitiesLength)
		if len(communities) > 0 {
			if err := utils.BinaryDecoder(payload, communities); err != nil {
				return flowRecord, &RecordError{(*header).DataFormat, err}
			}
		}
		if err := utils.BinaryDecoder(payload, &(extendedGateway.LocalPref)); err != nil {
			return flowRecord, &RecordError{(*header).DataFormat, err}
		}
		extendedGateway.Communities = communities

		flowRecord.Data = extendedGateway
	default:
		return flowRecord, &RecordError{(*header).DataFormat, fmt.Errorf("unknown data format")}
	}
	return flowRecord, nil
}

func DecodeSample(header *SampleHeader, payload *bytes.Buffer) (interface{}, error) {
	format := (*header).Format
	var sample interface{}

	if err := utils.BinaryDecoder(payload, &((*header).SampleSequenceNumber)); err != nil {
		return sample, &FlowError{format, fmt.Errorf("header seq [%w]", err)}
	}
	if format == FORMAT_RAW_PKT || format == FORMAT_ETH {
		var sourceId uint32
		if err := utils.BinaryDecoder(payload, &sourceId); err != nil {
			return sample, &FlowError{format, fmt.Errorf("header source [%w]", err)}
		}

		(*header).SourceIdType = sourceId >> 24
		(*header).SourceIdValue = sourceId & 0x00ffffff
	} else if format == FORMAT_IPV4 || format == FORMAT_IPV6 {
		if err := utils.BinaryDecoder(payload, &((*header).SourceIdType), &((*header).SourceIdValue)); err != nil {
			return sample, &FlowError{format, fmt.Errorf("header source [%w]", err)}
		}
	} else {
		return sample, &FlowError{format, fmt.Errorf("unknown format %d", format)}
	}

	var recordsCount uint32
	var flowSample FlowSample
	var counterSample CounterSample
	var expandedFlowSample ExpandedFlowSample
	if format == FORMAT_RAW_PKT {
		flowSample = FlowSample{
			Header: *header,
		}
		if err := utils.BinaryDecoder(payload, &(flowSample.SamplingRate), &(flowSample.SamplePool),
			&(flowSample.Drops), &(flowSample.Input), &(flowSample.Output), &(flowSample.FlowRecordsCount)); err != nil {
			return sample, &FlowError{format, fmt.Errorf("raw [%w]", err)}
		}
		recordsCount = flowSample.FlowRecordsCount
		flowSample.Records = make([]FlowRecord, recordsCount)
		sample = flowSample
	} else if format == FORMAT_ETH || format == FORMAT_IPV6 {
		if err := utils.BinaryDecoder(payload, &recordsCount); err != nil {
			return sample, &FlowError{format, fmt.Errorf("eth [%w]", err)}
		}
		counterSample = CounterSample{
			Header:              *header,
			CounterRecordsCount: recordsCount,
		}
		counterSample.Records = make([]CounterRecord, recordsCount)
		sample = counterSample
	} else if format == FORMAT_IPV4 {
		expandedFlowSample = ExpandedFlowSample{
			Header: *header,
		}
		if err := utils.BinaryDecoder(payload, &(expandedFlowSample.SamplingRate), &(expandedFlowSample.SamplePool),
			&(expandedFlowSample.Drops), &(expandedFlowSample.InputIfFormat), &(expandedFlowSample.InputIfValue),
			&(expandedFlowSample.OutputIfFormat), &(expandedFlowSample.OutputIfValue), &(expandedFlowSample.FlowRecordsCount)); err != nil {
			return sample, &FlowError{format, fmt.Errorf("IPv4 [%w]", err)}
		}
		recordsCount = expandedFlowSample.FlowRecordsCount
		expandedFlowSample.Records = make([]FlowRecord, recordsCount)
		sample = expandedFlowSample
	}
	for i := 0; i < int(recordsCount) && payload.Len() >= 8; i++ {
		recordHeader := RecordHeader{}
		if err := utils.BinaryDecoder(payload, &(recordHeader.DataFormat), &(recordHeader.Length)); err != nil {
			return sample, &FlowError{format, fmt.Errorf("record header [%w]", err)}
		}
		if int(recordHeader.Length) > payload.Len() {
			break
		}
		recordReader := bytes.NewBuffer(payload.Next(int(recordHeader.Length)))
		if format == FORMAT_RAW_PKT || format == FORMAT_IPV4 {
			record, err := DecodeFlowRecord(&recordHeader, recordReader)
			if err != nil {
				// todo: return or continue?
				return sample, &FlowError{format, fmt.Errorf("record [%w]", err)}
			}
			if format == FORMAT_RAW_PKT {
				flowSample.Records[i] = record
			} else if format == FORMAT_IPV4 {
				expandedFlowSample.Records[i] = record
			}
		} else if format == FORMAT_ETH || format == FORMAT_IPV6 {
			record, err := DecodeCounterRecord(&recordHeader, recordReader)
			if err != nil {
				// todo: return or continue?
				return sample, &FlowError{format, fmt.Errorf("counter [%w]", err)}
			}
			counterSample.Records[i] = record
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

	//if version == 5 {
	if err := utils.BinaryDecoder(payload, &(packetV5.IPVersion)); err != nil {
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
		&(packetV5.SubAgentId),
		&(packetV5.SequenceNumber),
		&(packetV5.Uptime),
		&(packetV5.SamplesCount)); err != nil {
		return &DecoderError{err}
	}
	packetV5.Samples = make([]interface{}, int(packetV5.SamplesCount))
	for i := 0; i < int(packetV5.SamplesCount) && payload.Len() >= 8; i++ {
		header := SampleHeader{}
		if err := utils.BinaryDecoder(payload, &(header.Format), &(header.Length)); err != nil {
			return &DecoderError{fmt.Errorf("header [%w]", err)}
		}
		if int(header.Length) > payload.Len() {
			break
		}
		sampleReader := bytes.NewBuffer(payload.Next(int(header.Length)))

		sample, err := DecodeSample(&header, sampleReader)
		if err != nil {
			// todo: investigate if better as log
			return &DecoderError{fmt.Errorf("sample [%w]", err)}
		} else {
			packetV5.Samples[i] = sample
		}
	}

	return nil
}

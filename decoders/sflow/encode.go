package sflow

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/netsampler/goflow2/v2/decoders/utils"
)

func (p *Packet) MarshalBinary() ([]byte, error) {
	return EncodeMessage(p)
}

func EncodeMessage(packet *Packet) ([]byte, error) {
	if packet == nil {
		return nil, errors.New("sflow: nil packet")
	}

	version := packet.Version
	if version == 0 {
		version = 5
	}
	if version != 5 {
		return nil, fmt.Errorf("sflow: unsupported version %d", version)
	}

	ipVersion := packet.IPVersion
	agentIP := []byte(packet.AgentIP)
	if ipVersion == 0 {
		switch len(agentIP) {
		case 4:
			ipVersion = 1
		case 16:
			ipVersion = 2
		default:
			return nil, fmt.Errorf("sflow: invalid agent IP length %d", len(agentIP))
		}
	}
	if (ipVersion == 1 && len(agentIP) != 4) || (ipVersion == 2 && len(agentIP) != 16) {
		return nil, fmt.Errorf("sflow: agent IP length %d does not match ip-version %d", len(agentIP), ipVersion)
	}

	samplesCount := packet.SamplesCount
	if samplesCount == 0 {
		samplesCount = uint32(len(packet.Samples))
	}
	if int(samplesCount) != len(packet.Samples) {
		return nil, fmt.Errorf("sflow: samples-count mismatch header:%d samples:%d", samplesCount, len(packet.Samples))
	}

	buf := bytes.NewBuffer(nil)
	if err := utils.WriteU32(buf, version); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, ipVersion); err != nil {
		return nil, err
	}
	if _, err := buf.Write(agentIP); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.SubAgentId); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.SequenceNumber); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.Uptime); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, samplesCount); err != nil {
		return nil, err
	}

	for _, sample := range packet.Samples {
		samplePayload, format, err := encodeSample(sample)
		if err != nil {
			return nil, err
		}
		if err := utils.WriteU32(buf, format); err != nil {
			return nil, err
		}
		if err := utils.WriteU32(buf, uint32(len(samplePayload))); err != nil {
			return nil, err
		}
		if _, err := buf.Write(samplePayload); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

func encodeSample(sample interface{}) ([]byte, uint32, error) {
	switch s := sample.(type) {
	case FlowSample:
		format := s.Header.Format
		if format == 0 {
			format = SAMPLE_FORMAT_FLOW
		}
		payload, err := encodeFlowSample(&s, format)
		return payload, format, err
	case *FlowSample:
		format := s.Header.Format
		if format == 0 {
			format = SAMPLE_FORMAT_FLOW
		}
		payload, err := encodeFlowSample(s, format)
		return payload, format, err
	case CounterSample:
		format := s.Header.Format
		if format == 0 {
			format = SAMPLE_FORMAT_COUNTER
		}
		payload, err := encodeCounterSample(&s, format)
		return payload, format, err
	case *CounterSample:
		format := s.Header.Format
		if format == 0 {
			format = SAMPLE_FORMAT_COUNTER
		}
		payload, err := encodeCounterSample(s, format)
		return payload, format, err
	case ExpandedFlowSample:
		format := s.Header.Format
		if format == 0 {
			format = SAMPLE_FORMAT_EXPANDED_FLOW
		}
		payload, err := encodeExpandedFlowSample(&s, format)
		return payload, format, err
	case *ExpandedFlowSample:
		format := s.Header.Format
		if format == 0 {
			format = SAMPLE_FORMAT_EXPANDED_FLOW
		}
		payload, err := encodeExpandedFlowSample(s, format)
		return payload, format, err
	case DropSample:
		format := s.Header.Format
		if format == 0 {
			format = SAMPLE_FORMAT_DROP
		}
		payload, err := encodeDropSample(&s, format)
		return payload, format, err
	case *DropSample:
		format := s.Header.Format
		if format == 0 {
			format = SAMPLE_FORMAT_DROP
		}
		payload, err := encodeDropSample(s, format)
		return payload, format, err
	default:
		return nil, 0, fmt.Errorf("sflow: unsupported sample type %T", sample)
	}
}

func encodeFlowSample(sample *FlowSample, format uint32) ([]byte, error) {
	if format != SAMPLE_FORMAT_FLOW {
		return nil, fmt.Errorf("sflow: invalid flow sample format %d", format)
	}
	buf := bytes.NewBuffer(nil)
	if err := encodeSampleHeader(buf, &sample.Header, format); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.SamplingRate); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.SamplePool); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.Drops); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.Input); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.Output); err != nil {
		return nil, err
	}
	recordsCount := sample.FlowRecordsCount
	if recordsCount == 0 {
		recordsCount = uint32(len(sample.Records))
	}
	if int(recordsCount) != len(sample.Records) {
		return nil, fmt.Errorf("sflow: flow records-count mismatch header:%d records:%d", recordsCount, len(sample.Records))
	}
	if err := utils.WriteU32(buf, recordsCount); err != nil {
		return nil, err
	}
	for _, record := range sample.Records {
		if err := encodeFlowRecord(buf, &record); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func encodeExpandedFlowSample(sample *ExpandedFlowSample, format uint32) ([]byte, error) {
	if format != SAMPLE_FORMAT_EXPANDED_FLOW {
		return nil, fmt.Errorf("sflow: invalid expanded flow sample format %d", format)
	}
	buf := bytes.NewBuffer(nil)
	if err := encodeSampleHeader(buf, &sample.Header, format); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.SamplingRate); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.SamplePool); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.Drops); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.InputIfFormat); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.InputIfValue); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.OutputIfFormat); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.OutputIfValue); err != nil {
		return nil, err
	}
	recordsCount := sample.FlowRecordsCount
	if recordsCount == 0 {
		recordsCount = uint32(len(sample.Records))
	}
	if int(recordsCount) != len(sample.Records) {
		return nil, fmt.Errorf("sflow: expanded flow records-count mismatch header:%d records:%d", recordsCount, len(sample.Records))
	}
	if err := utils.WriteU32(buf, recordsCount); err != nil {
		return nil, err
	}
	for _, record := range sample.Records {
		if err := encodeFlowRecord(buf, &record); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func encodeDropSample(sample *DropSample, format uint32) ([]byte, error) {
	if format != SAMPLE_FORMAT_DROP {
		return nil, fmt.Errorf("sflow: invalid drop sample format %d", format)
	}
	buf := bytes.NewBuffer(nil)
	if err := encodeSampleHeader(buf, &sample.Header, format); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.Drops); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.Input); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.Output); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, sample.Reason); err != nil {
		return nil, err
	}
	recordsCount := sample.FlowRecordsCount
	if recordsCount == 0 {
		recordsCount = uint32(len(sample.Records))
	}
	if int(recordsCount) != len(sample.Records) {
		return nil, fmt.Errorf("sflow: drop records-count mismatch header:%d records:%d", recordsCount, len(sample.Records))
	}
	if err := utils.WriteU32(buf, recordsCount); err != nil {
		return nil, err
	}
	for _, record := range sample.Records {
		if err := encodeFlowRecord(buf, &record); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func encodeCounterSample(sample *CounterSample, format uint32) ([]byte, error) {
	if format != SAMPLE_FORMAT_COUNTER && format != SAMPLE_FORMAT_EXPANDED_COUNTER {
		return nil, fmt.Errorf("sflow: invalid counter sample format %d", format)
	}
	buf := bytes.NewBuffer(nil)
	if err := encodeSampleHeader(buf, &sample.Header, format); err != nil {
		return nil, err
	}
	recordsCount := sample.CounterRecordsCount
	if recordsCount == 0 {
		recordsCount = uint32(len(sample.Records))
	}
	if int(recordsCount) != len(sample.Records) {
		return nil, fmt.Errorf("sflow: counter records-count mismatch header:%d records:%d", recordsCount, len(sample.Records))
	}
	if err := utils.WriteU32(buf, recordsCount); err != nil {
		return nil, err
	}
	for _, record := range sample.Records {
		if err := encodeCounterRecord(buf, &record); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func encodeSampleHeader(buf *bytes.Buffer, header *SampleHeader, format uint32) error {
	if err := utils.WriteU32(buf, header.SampleSequenceNumber); err != nil {
		return err
	}
	switch format {
	case SAMPLE_FORMAT_FLOW, SAMPLE_FORMAT_COUNTER:
		sourceId := (header.SourceIdType << 24) | (header.SourceIdValue & 0x00ffffff)
		if err := utils.WriteU32(buf, sourceId); err != nil {
			return err
		}
	case SAMPLE_FORMAT_EXPANDED_FLOW, SAMPLE_FORMAT_EXPANDED_COUNTER, SAMPLE_FORMAT_DROP:
		if err := utils.WriteU32(buf, header.SourceIdType); err != nil {
			return err
		}
		if err := utils.WriteU32(buf, header.SourceIdValue); err != nil {
			return err
		}
	default:
		return fmt.Errorf("sflow: unknown sample format %d", format)
	}
	return nil
}

func encodeFlowRecord(buf *bytes.Buffer, record *FlowRecord) error {
	payload := bytes.NewBuffer(nil)
	dataFormat := record.Header.DataFormat

	switch data := record.Data.(type) {
	case SampledHeader:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_RAW
		}
		if err := utils.WriteU32(payload, data.Protocol); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.FrameLength); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Stripped); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.OriginalLength); err != nil {
			return err
		}
		if _, err := payload.Write(data.HeaderData); err != nil {
			return err
		}
	case *SampledHeader:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_RAW
		}
		if err := utils.WriteU32(payload, data.Protocol); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.FrameLength); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Stripped); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.OriginalLength); err != nil {
			return err
		}
		if _, err := payload.Write(data.HeaderData); err != nil {
			return err
		}
	case SampledEthernet:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_ETH
		}
		if len(data.SrcMac) != 6 || len(data.DstMac) != 6 {
			return fmt.Errorf("sflow: ethernet MAC length must be 6")
		}
		if err := utils.WriteU32(payload, data.Length); err != nil {
			return err
		}
		if _, err := payload.Write(data.SrcMac); err != nil {
			return err
		}
		if _, err := payload.Write(data.DstMac); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.EthType); err != nil {
			return err
		}
	case *SampledEthernet:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_ETH
		}
		if len(data.SrcMac) != 6 || len(data.DstMac) != 6 {
			return fmt.Errorf("sflow: ethernet MAC length must be 6")
		}
		if err := utils.WriteU32(payload, data.Length); err != nil {
			return err
		}
		if _, err := payload.Write(data.SrcMac); err != nil {
			return err
		}
		if _, err := payload.Write(data.DstMac); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.EthType); err != nil {
			return err
		}
	case SampledIPv4:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_IPV4
		}
		if len(data.SrcIP) != 4 || len(data.DstIP) != 4 {
			return fmt.Errorf("sflow: IPv4 length must be 4")
		}
		if err := encodeSampledIPBase(payload, data.SampledIPBase); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Tos); err != nil {
			return err
		}
	case *SampledIPv4:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_IPV4
		}
		if len(data.SrcIP) != 4 || len(data.DstIP) != 4 {
			return fmt.Errorf("sflow: IPv4 length must be 4")
		}
		if err := encodeSampledIPBase(payload, data.SampledIPBase); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Tos); err != nil {
			return err
		}
	case SampledIPv6:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_IPV6
		}
		if len(data.SrcIP) != 16 || len(data.DstIP) != 16 {
			return fmt.Errorf("sflow: IPv6 length must be 16")
		}
		if err := encodeSampledIPBase(payload, data.SampledIPBase); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Priority); err != nil {
			return err
		}
	case *SampledIPv6:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_IPV6
		}
		if len(data.SrcIP) != 16 || len(data.DstIP) != 16 {
			return fmt.Errorf("sflow: IPv6 length must be 16")
		}
		if err := encodeSampledIPBase(payload, data.SampledIPBase); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Priority); err != nil {
			return err
		}
	case ExtendedSwitch:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_EXT_SWITCH
		}
		if err := utils.WriteU32(payload, data.SrcVlan); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.SrcPriority); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.DstVlan); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.DstPriority); err != nil {
			return err
		}
	case *ExtendedSwitch:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_EXT_SWITCH
		}
		if err := utils.WriteU32(payload, data.SrcVlan); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.SrcPriority); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.DstVlan); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.DstPriority); err != nil {
			return err
		}
	case ExtendedRouter:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_EXT_ROUTER
		}
		if err := encodeIP(payload, data.NextHopIPVersion, data.NextHop); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.SrcMaskLen); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.DstMaskLen); err != nil {
			return err
		}
	case *ExtendedRouter:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_EXT_ROUTER
		}
		if err := encodeIP(payload, data.NextHopIPVersion, data.NextHop); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.SrcMaskLen); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.DstMaskLen); err != nil {
			return err
		}
	case ExtendedGateway:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_EXT_GATEWAY
		}
		if err := encodeExtendedGateway(payload, &data); err != nil {
			return err
		}
	case *ExtendedGateway:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_EXT_GATEWAY
		}
		if err := encodeExtendedGateway(payload, data); err != nil {
			return err
		}
	case EgressQueue:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_EGRESS_QUEUE
		}
		if err := utils.WriteU32(payload, data.Queue); err != nil {
			return err
		}
	case *EgressQueue:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_EGRESS_QUEUE
		}
		if err := utils.WriteU32(payload, data.Queue); err != nil {
			return err
		}
	case ExtendedACL:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_EXT_ACL
		}
		if err := utils.WriteU32(payload, data.Number); err != nil {
			return err
		}
		if err := utils.WriteString(payload, data.Name); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Direction); err != nil {
			return err
		}
	case *ExtendedACL:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_EXT_ACL
		}
		if err := utils.WriteU32(payload, data.Number); err != nil {
			return err
		}
		if err := utils.WriteString(payload, data.Name); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Direction); err != nil {
			return err
		}
	case ExtendedFunction:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_EXT_FUNCTION
		}
		if err := utils.WriteString(payload, data.Symbol); err != nil {
			return err
		}
	case *ExtendedFunction:
		if dataFormat == 0 {
			dataFormat = FLOW_TYPE_EXT_FUNCTION
		}
		if err := utils.WriteString(payload, data.Symbol); err != nil {
			return err
		}
	case RawRecord:
		if dataFormat == 0 {
			dataFormat = record.Header.DataFormat
		}
		if _, err := payload.Write(data.Data); err != nil {
			return err
		}
	case *RawRecord:
		if dataFormat == 0 {
			dataFormat = record.Header.DataFormat
		}
		if _, err := payload.Write(data.Data); err != nil {
			return err
		}
	default:
		return fmt.Errorf("sflow: unsupported flow record type %T", record.Data)
	}

	if dataFormat == 0 {
		return fmt.Errorf("sflow: flow record data-format is not set")
	}
	if err := utils.WriteU32(buf, dataFormat); err != nil {
		return err
	}
	if err := utils.WriteU32(buf, uint32(payload.Len())); err != nil {
		return err
	}
	_, err := buf.Write(payload.Bytes())
	return err
}

func encodeCounterRecord(buf *bytes.Buffer, record *CounterRecord) error {
	payload := bytes.NewBuffer(nil)
	dataFormat := record.Header.DataFormat

	switch data := record.Data.(type) {
	case IfCounters:
		if dataFormat == 0 {
			dataFormat = COUNTER_TYPE_IF
		}
		if err := utils.WriteU32(payload, data.IfIndex); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfType); err != nil {
			return err
		}
		if err := utils.WriteU64(payload, data.IfSpeed); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfDirection); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfStatus); err != nil {
			return err
		}
		if err := utils.WriteU64(payload, data.IfInOctets); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfInUcastPkts); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfInMulticastPkts); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfInBroadcastPkts); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfInDiscards); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfInErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfInUnknownProtos); err != nil {
			return err
		}
		if err := utils.WriteU64(payload, data.IfOutOctets); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfOutUcastPkts); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfOutMulticastPkts); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfOutBroadcastPkts); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfOutDiscards); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfOutErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfPromiscuousMode); err != nil {
			return err
		}
	case *IfCounters:
		if dataFormat == 0 {
			dataFormat = COUNTER_TYPE_IF
		}
		if err := utils.WriteU32(payload, data.IfIndex); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfType); err != nil {
			return err
		}
		if err := utils.WriteU64(payload, data.IfSpeed); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfDirection); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfStatus); err != nil {
			return err
		}
		if err := utils.WriteU64(payload, data.IfInOctets); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfInUcastPkts); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfInMulticastPkts); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfInBroadcastPkts); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfInDiscards); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfInErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfInUnknownProtos); err != nil {
			return err
		}
		if err := utils.WriteU64(payload, data.IfOutOctets); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfOutUcastPkts); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfOutMulticastPkts); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfOutBroadcastPkts); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfOutDiscards); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfOutErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.IfPromiscuousMode); err != nil {
			return err
		}
	case EthernetCounters:
		if dataFormat == 0 {
			dataFormat = COUNTER_TYPE_ETH
		}
		if err := utils.WriteU32(payload, data.Dot3StatsAlignmentErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsFCSErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsSingleCollisionFrames); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsMultipleCollisionFrames); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsSQETestErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsDeferredTransmissions); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsLateCollisions); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsExcessiveCollisions); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsInternalMacTransmitErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsCarrierSenseErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsFrameTooLongs); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsInternalMacReceiveErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsSymbolErrors); err != nil {
			return err
		}
	case *EthernetCounters:
		if dataFormat == 0 {
			dataFormat = COUNTER_TYPE_ETH
		}
		if err := utils.WriteU32(payload, data.Dot3StatsAlignmentErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsFCSErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsSingleCollisionFrames); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsMultipleCollisionFrames); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsSQETestErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsDeferredTransmissions); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsLateCollisions); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsExcessiveCollisions); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsInternalMacTransmitErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsCarrierSenseErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsFrameTooLongs); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsInternalMacReceiveErrors); err != nil {
			return err
		}
		if err := utils.WriteU32(payload, data.Dot3StatsSymbolErrors); err != nil {
			return err
		}
	case RawRecord:
		if dataFormat == 0 {
			dataFormat = record.Header.DataFormat
		}
		if _, err := payload.Write(data.Data); err != nil {
			return err
		}
	case *RawRecord:
		if dataFormat == 0 {
			dataFormat = record.Header.DataFormat
		}
		if _, err := payload.Write(data.Data); err != nil {
			return err
		}
	default:
		return fmt.Errorf("sflow: unsupported counter record type %T", record.Data)
	}

	if dataFormat == 0 {
		return fmt.Errorf("sflow: counter record data-format is not set")
	}
	if err := utils.WriteU32(buf, dataFormat); err != nil {
		return err
	}
	if err := utils.WriteU32(buf, uint32(payload.Len())); err != nil {
		return err
	}
	_, err := buf.Write(payload.Bytes())
	return err
}

func encodeSampledIPBase(buf *bytes.Buffer, data SampledIPBase) error {
	if err := utils.WriteU32(buf, data.Length); err != nil {
		return err
	}
	if err := utils.WriteU32(buf, data.Protocol); err != nil {
		return err
	}
	if _, err := buf.Write(data.SrcIP); err != nil {
		return err
	}
	if _, err := buf.Write(data.DstIP); err != nil {
		return err
	}
	if err := utils.WriteU32(buf, data.SrcPort); err != nil {
		return err
	}
	if err := utils.WriteU32(buf, data.DstPort); err != nil {
		return err
	}
	if err := utils.WriteU32(buf, data.TcpFlags); err != nil {
		return err
	}
	return nil
}

func encodeExtendedGateway(buf *bytes.Buffer, data *ExtendedGateway) error {
	if err := encodeIP(buf, data.NextHopIPVersion, data.NextHop); err != nil {
		return err
	}
	if err := utils.WriteU32(buf, data.AS); err != nil {
		return err
	}
	if err := utils.WriteU32(buf, data.SrcAS); err != nil {
		return err
	}
	if err := utils.WriteU32(buf, data.SrcPeerAS); err != nil {
		return err
	}
	if err := utils.WriteU32(buf, data.ASDestinations); err != nil {
		return err
	}

	asPathLen := data.ASPathLength
	if asPathLen == 0 && len(data.ASPath) > 0 {
		asPathLen = uint32(len(data.ASPath))
	}
	if asPathLen > 0 {
		if int(asPathLen) != len(data.ASPath) {
			return fmt.Errorf("sflow: AS path length mismatch header:%d path:%d", asPathLen, len(data.ASPath))
		}
		if err := utils.WriteU32(buf, data.ASPathType); err != nil {
			return err
		}
		if err := utils.WriteU32(buf, asPathLen); err != nil {
			return err
		}
		for _, asn := range data.ASPath {
			if err := utils.WriteU32(buf, asn); err != nil {
				return err
			}
		}
	}

	commLen := data.CommunitiesLength
	if commLen == 0 && len(data.Communities) > 0 {
		commLen = uint32(len(data.Communities))
	}
	if int(commLen) != len(data.Communities) {
		return fmt.Errorf("sflow: communities length mismatch header:%d communities:%d", commLen, len(data.Communities))
	}
	if err := utils.WriteU32(buf, commLen); err != nil {
		return err
	}
	for _, community := range data.Communities {
		if err := utils.WriteU32(buf, community); err != nil {
			return err
		}
	}

	if err := utils.WriteU32(buf, data.LocalPref); err != nil {
		return err
	}
	return nil
}

func encodeIP(buf *bytes.Buffer, ipVersion uint32, ip []byte) error {
	if ipVersion == 0 {
		switch len(ip) {
		case 4:
			ipVersion = 1
		case 16:
			ipVersion = 2
		default:
			return fmt.Errorf("sflow: invalid IP length %d", len(ip))
		}
	}
	if (ipVersion == 1 && len(ip) != 4) || (ipVersion == 2 && len(ip) != 16) {
		return fmt.Errorf("sflow: IP length %d does not match ip-version %d", len(ip), ipVersion)
	}
	if err := utils.WriteU32(buf, ipVersion); err != nil {
		return err
	}
	_, err := buf.Write(ip)
	return err
}

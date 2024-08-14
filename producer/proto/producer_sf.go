package protoproducer

import (
	"github.com/netsampler/goflow2/v2/decoders/sflow"
	flowmessage "github.com/netsampler/goflow2/v2/pb"
	"github.com/netsampler/goflow2/v2/producer"
)

func GetSFlowFlowSamples(packet *sflow.Packet) []interface{} {
	var flowSamples []interface{}
	for _, sample := range packet.Samples {
		switch sample.(type) {
		case sflow.FlowSample:
			flowSamples = append(flowSamples, sample)
		case sflow.ExpandedFlowSample:
			flowSamples = append(flowSamples, sample)
		}
	}
	return flowSamples
}

func ParseSampledHeader(flowMessage *ProtoProducerMessage, sampledHeader *sflow.SampledHeader) error {
	return ParseSampledHeaderConfig(flowMessage, sampledHeader, nil)
}

func ParseSampledHeaderConfig(flowMessage *ProtoProducerMessage, sampledHeader *sflow.SampledHeader, config PacketMapper) error {
	data := (*sampledHeader).HeaderData
	switch (*sampledHeader).Protocol {
	case 1: // Ethernet
		if config == nil {
			config = DefaultEnvironment
		}

		if err := config.ParsePacket(flowMessage, data); err != nil {
			return err
		}
	}
	return nil
}

func SearchSFlowSampleConfig(flowMessage *ProtoProducerMessage, flowSample interface{}, config PacketMapper) error {
	var records []sflow.FlowRecord
	flowMessage.Type = flowmessage.FlowMessage_SFLOW_5

	switch flowSample := flowSample.(type) {
	case sflow.FlowSample:
		records = flowSample.Records
		flowMessage.SamplingRate = uint64(flowSample.SamplingRate)
		flowMessage.InIf = flowSample.Input
		flowMessage.OutIf = flowSample.Output
	case sflow.ExpandedFlowSample:
		records = flowSample.Records
		flowMessage.SamplingRate = uint64(flowSample.SamplingRate)
		flowMessage.InIf = flowSample.InputIfValue
		flowMessage.OutIf = flowSample.OutputIfValue
	}

	var ipNh, ipSrc, ipDst []byte
	flowMessage.Packets = 1
	for _, record := range records {
		switch recordData := record.Data.(type) {
		case sflow.SampledHeader:
			flowMessage.Bytes = uint64(recordData.FrameLength)
			if err := ParseSampledHeaderConfig(flowMessage, &recordData, config); err != nil { // todo: make function configurable
				return err
			}
		case sflow.SampledIPv4:
			ipSrc = recordData.SrcIP
			ipDst = recordData.DstIP
			flowMessage.SrcAddr = ipSrc
			flowMessage.DstAddr = ipDst
			flowMessage.Bytes = uint64(recordData.Length)
			flowMessage.Proto = recordData.Protocol
			flowMessage.SrcPort = recordData.SrcPort
			flowMessage.DstPort = recordData.DstPort
			flowMessage.IpTos = recordData.Tos
			flowMessage.Etype = 0x800
		case sflow.SampledIPv6:
			ipSrc = recordData.SrcIP
			ipDst = recordData.DstIP
			flowMessage.SrcAddr = ipSrc
			flowMessage.DstAddr = ipDst
			flowMessage.Bytes = uint64(recordData.Length)
			flowMessage.Proto = recordData.Protocol
			flowMessage.SrcPort = recordData.SrcPort
			flowMessage.DstPort = recordData.DstPort
			flowMessage.IpTos = recordData.Priority
			flowMessage.Etype = 0x86dd
		case sflow.ExtendedRouter:
			ipNh = recordData.NextHop
			flowMessage.NextHop = ipNh
			flowMessage.SrcNet = recordData.SrcMaskLen
			flowMessage.DstNet = recordData.DstMaskLen
		case sflow.ExtendedGateway:
			ipNh = recordData.NextHop
			flowMessage.BgpNextHop = ipNh
			flowMessage.BgpCommunities = recordData.Communities
			flowMessage.AsPath = recordData.ASPath
			if len(recordData.ASPath) > 0 {
				flowMessage.DstAs = recordData.ASPath[len(recordData.ASPath)-1]
				flowMessage.NextHopAs = recordData.ASPath[0]
			} else {
				flowMessage.DstAs = recordData.AS
			}
			if recordData.SrcAS > 0 {
				flowMessage.SrcAs = recordData.SrcAS
			} else {
				flowMessage.SrcAs = recordData.AS
			}
		case sflow.ExtendedSwitch:
			flowMessage.SrcVlan = recordData.SrcVlan
			flowMessage.DstVlan = recordData.DstVlan
		}
	}
	return nil

}

func SearchSFlowSamplesConfig(samples []interface{}, config PacketMapper) (flowMessageSet []producer.ProducerMessage, err error) {
	for _, flowSample := range samples {
		fmsg := protoMessagePool.Get().(*ProtoProducerMessage)
		fmsg.Reset()
		if err := SearchSFlowSampleConfig(fmsg, flowSample, config); err != nil {
			return nil, err
		}
		flowMessageSet = append(flowMessageSet, fmsg)
	}
	return flowMessageSet, nil
}

// Converts an sFlow message
func ProcessMessageSFlowConfig(packet *sflow.Packet, config ProtoProducerConfig) (flowMessageSet []producer.ProducerMessage, err error) {
	seqnum := packet.SequenceNumber
	agent := packet.AgentIP

	var cfgSFlow PacketMapper
	if config != nil {
		cfgSFlow = config.GetPacketMapper()
	}

	flowSamples := GetSFlowFlowSamples(packet)
	flowMessageSet, err = SearchSFlowSamplesConfig(flowSamples, cfgSFlow)
	if err != nil {
		return flowMessageSet, err
	}
	for _, msg := range flowMessageSet {
		fmsg, ok := msg.(*ProtoProducerMessage)
		if !ok {
			continue
		}
		fmsg.SamplerAddress = agent
		fmsg.SequenceNum = seqnum
	}

	return flowMessageSet, nil
}

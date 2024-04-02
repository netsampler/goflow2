package protoproducer

import (
	"encoding/binary"

	"github.com/netsampler/goflow2/v2/decoders/netflowlegacy"
	flowmessage "github.com/netsampler/goflow2/v2/pb"
	"github.com/netsampler/goflow2/v2/producer"
)

func ConvertNetFlowLegacyRecord(flowMessage *ProtoProducerMessage, baseTime uint64, uptime uint32, record netflowlegacy.RecordsNetFlowV5) {
	flowMessage.Type = flowmessage.FlowMessage_NETFLOW_V5

	timeDiffFirst := (uptime - record.First)
	timeDiffLast := (uptime - record.Last)
	flowMessage.TimeFlowStartNs = baseTime - uint64(timeDiffFirst)*1000000
	flowMessage.TimeFlowEndNs = baseTime - uint64(timeDiffLast)*1000000

	v := make([]byte, 4)
	binary.BigEndian.PutUint32(v, uint32(record.NextHop))
	flowMessage.NextHop = v
	v = make([]byte, 4)
	binary.BigEndian.PutUint32(v, uint32(record.SrcAddr))
	flowMessage.SrcAddr = v
	v = make([]byte, 4)
	binary.BigEndian.PutUint32(v, uint32(record.DstAddr))
	flowMessage.DstAddr = v

	flowMessage.Etype = 0x800
	flowMessage.SrcAs = uint32(record.SrcAS)
	flowMessage.DstAs = uint32(record.DstAS)
	flowMessage.SrcNet = uint32(record.SrcMask)
	flowMessage.DstNet = uint32(record.DstMask)
	flowMessage.Proto = uint32(record.Proto)
	flowMessage.TcpFlags = uint32(record.TCPFlags)
	flowMessage.IpTos = uint32(record.Tos)
	flowMessage.InIf = uint32(record.Input)
	flowMessage.OutIf = uint32(record.Output)
	flowMessage.SrcPort = uint32(record.SrcPort)
	flowMessage.DstPort = uint32(record.DstPort)
	flowMessage.Packets = uint64(record.DPkts)
	flowMessage.Bytes = uint64(record.DOctets)
}

func SearchNetFlowLegacyRecords(baseTime uint64, uptime uint32, dataRecords []netflowlegacy.RecordsNetFlowV5) (flowMessageSet []producer.ProducerMessage) {
	for _, record := range dataRecords {
		fmsg := protoMessagePool.Get().(*ProtoProducerMessage)
		fmsg.Reset()
		ConvertNetFlowLegacyRecord(fmsg, baseTime, uptime, record)
		flowMessageSet = append(flowMessageSet, fmsg)
	}
	return flowMessageSet
}

func ProcessMessageNetFlowLegacy(packet *netflowlegacy.PacketNetFlowV5) ([]producer.ProducerMessage, error) {
	seqnum := packet.FlowSequence
	samplingRate := packet.SamplingInterval & 0x3FFF
	baseTime := uint64(packet.UnixSecs)*1000000000 + uint64(packet.UnixNSecs)
	uptime := packet.SysUptime

	flowMessageSet := SearchNetFlowLegacyRecords(baseTime, uptime, packet.Records)
	for _, msg := range flowMessageSet {
		fmsg, ok := msg.(*ProtoProducerMessage)
		if !ok {
			continue
		}
		fmsg.SequenceNum = seqnum
		fmsg.SamplingRate = uint64(samplingRate)
	}

	return flowMessageSet, nil
}

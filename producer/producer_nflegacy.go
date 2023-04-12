package producer

import (
	"encoding/binary"
	"net"

	"github.com/netsampler/goflow2/decoders/netflowlegacy"
	flowmessage "github.com/netsampler/goflow2/pb"
)

func ConvertNetFlowLegacyRecord(flowMessage *ProtoProducerMessage, baseTime uint32, uptime uint32, record netflowlegacy.RecordsNetFlowV5) {
	flowMessage.Type = flowmessage.FlowMessage_NETFLOW_V5

	timeDiffFirst := (uptime - record.First)
	timeDiffLast := (uptime - record.Last)
	flowMessage.TimeFlowStartMs = uint64(baseTime)*1000 - uint64(timeDiffFirst)
	flowMessage.TimeFlowEndMs = uint64(baseTime)*1000 - uint64(timeDiffLast)

	v := make(net.IP, 4)
	binary.BigEndian.PutUint32(v, record.NextHop)
	flowMessage.NextHop = v
	v = make(net.IP, 4)
	binary.BigEndian.PutUint32(v, record.SrcAddr)
	flowMessage.SrcAddr = v
	v = make(net.IP, 4)
	binary.BigEndian.PutUint32(v, record.DstAddr)
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

func SearchNetFlowLegacyRecords(baseTime uint32, uptime uint32, dataRecords []netflowlegacy.RecordsNetFlowV5) []ProducerMessage {
	var flowMessageSet []ProducerMessage
	for _, record := range dataRecords {
		fmsg := protoMessagePool.Get().(*ProtoProducerMessage)
		fmsg.Reset()
		ConvertNetFlowLegacyRecord(fmsg, baseTime, uptime, record)
		flowMessageSet = append(flowMessageSet, fmsg)
	}
	return flowMessageSet
}

func ProcessMessageNetFlowLegacy(packet *netflowlegacy.PacketNetFlowV5) ([]ProducerMessage, error) {
	seqnum := packet.FlowSequence
	samplingRate := packet.SamplingInterval
	baseTime := packet.UnixSecs
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

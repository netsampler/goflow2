package encode

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

type NFv5Encoder struct {
	seq atomic.Uint32
}

func NewNFv5Encoder(cfg config.EncoderConfig) *NFv5Encoder {
	return &NFv5Encoder{}
}

// Encode turns one canonical event into one NetFlow v5 datagram.
func (e *NFv5Encoder) Encode(evt *event.Event) ([][]byte, error) {
	if evt == nil || evt.Kind == "control" {
		return nil, nil
	}

	packet, err := e.buildPacket(evt)
	if err != nil {
		return nil, err
	}
	data, err := netflowlegacy.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode netflow v5 packet: %w", err)
	}
	return [][]byte{data}, nil
}

// Flush is a no-op because the NetFlow v5 encoder is stateless.
func (e *NFv5Encoder) Flush() ([][]byte, error) {
	return nil, nil
}

// buildPacket converts one canonical event into a NetFlow v5 export packet.
func (e *NFv5Encoder) buildPacket(evt *event.Event) (*netflowlegacy.PacketNetFlowV5, error) {
	if evt == nil {
		return nil, fmt.Errorf("nil event")
	}
	if evt.Fields == nil {
		return nil, fmt.Errorf("event fields are empty")
	}

	exportMS := exportUnixMilliseconds(evt.ReceivedAt, evt.Fields)
	sysUptime, first, last := uptimeWindow(exportMS, int64Field(evt.Fields, "start_time_unix"), int64Field(evt.Fields, "end_time_unix"))
	exportTime := time.UnixMilli(exportMS).UTC()

	record := netflowlegacy.RecordsNetFlowV5{
		SrcAddr:  mustIPv4Address(evt.Fields, "src_addr"),
		DstAddr:  mustIPv4Address(evt.Fields, "dst_addr"),
		NextHop:  mustIPv4Address(evt.Fields, "next_hop"),
		Input:    uint16Field(evt.Fields, "input_if"),
		Output:   uint16Field(evt.Fields, "output_if"),
		DPkts:    uint32(uint64FromAny(evt.Fields["packets"])),
		DOctets:  uint32(uint64FromAny(evt.Fields["bytes"])),
		First:    first,
		Last:     last,
		SrcPort:  uint16Field(evt.Fields, "src_port"),
		DstPort:  uint16Field(evt.Fields, "dst_port"),
		TCPFlags: uint8(uint32Field(evt.Fields, "tcp_flags")),
		Proto:    uint8(uint32Field(evt.Fields, "proto")),
		Tos:      uint8(uint32Field(evt.Fields, "tos")),
		SrcAS:    uint16Field(evt.Fields, "src_as"),
		DstAS:    uint16Field(evt.Fields, "dst_as"),
		SrcMask:  uint8(uint32Field(evt.Fields, "src_mask")),
		DstMask:  uint8(uint32Field(evt.Fields, "dst_mask")),
	}

	return &netflowlegacy.PacketNetFlowV5{
		Version:          5,
		Count:            1,
		SysUptime:        sysUptime,
		UnixSecs:         uint32(exportTime.Unix()),
		UnixNSecs:        uint32(exportTime.Nanosecond()),
		FlowSequence:     e.seq.Add(1),
		EngineType:       uint8(uint32Field(evt.Fields, "engine_type")),
		EngineId:         uint8(uint32Field(evt.Fields, "engine_id")),
		SamplingInterval: uint16Field(evt.Fields, "sampling_rate"),
		Records:          []netflowlegacy.RecordsNetFlowV5{record},
	}, nil
}

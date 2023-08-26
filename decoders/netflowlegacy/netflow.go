package netflowlegacy

import (
	"bytes"
	"fmt"

	"github.com/netsampler/goflow2/decoders/utils"
)

const (
	MAX_COUNT = 1536
)

type ErrorVersion struct {
	version uint16
}

func NewErrorVersion(version uint16) *ErrorVersion {
	return &ErrorVersion{
		version: version,
	}
}

func (e *ErrorVersion) Error() string {
	return fmt.Sprintf("Unknown NetFlow version %v (only decodes v5)", e.version)
}

func DecodeMessage(payload *bytes.Buffer) (interface{}, error) {
	var version uint16
	err := utils.BinaryDecoder(payload, &version)
	if err != nil {
		return nil, err
	}
	packet := PacketNetFlowV5{}
	if version == 5 {
		packet.Version = version

		utils.BinaryDecoder(payload,
			&(packet.Count),
			&(packet.SysUptime),
			&(packet.UnixSecs),
			&(packet.UnixNSecs),
			&(packet.FlowSequence),
			&(packet.EngineType),
			&(packet.EngineId),
			&(packet.SamplingInterval),
		)

		packet.SamplingInterval = packet.SamplingInterval & 0x3FFF

		if packet.Count > MAX_COUNT {
			return nil, fmt.Errorf("Too many samples (%d > %d) in packet", packet.Count, MAX_COUNT)
		}

		packet.Records = make([]RecordsNetFlowV5, int(packet.Count))
		for i := 0; i < int(packet.Count) && payload.Len() >= 48; i++ {
			record := RecordsNetFlowV5{}
			err := utils.BinaryDecoder(payload,
				&record.SrcAddr,
				&record.DstAddr,
				&record.NextHop,
				&record.Input,
				&record.Output,
				&record.DPkts,
				&record.DOctets,
				&record.First,
				&record.Last,
				&record.SrcPort,
				&record.DstPort,
				&record.Pad1,
				&record.TCPFlags,
				&record.Proto,
				&record.Tos,
				&record.SrcAS,
				&record.DstAS,
				&record.SrcMask,
				&record.DstMask,
				&record.Pad2,
			)
			if err != nil {
				return packet, err
			}
			packet.Records[i] = record
		}

		return packet, nil
	} else {
		return nil, NewErrorVersion(version)
	}
}

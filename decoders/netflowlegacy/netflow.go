package netflowlegacy

import (
	"bytes"
	"fmt"

	"github.com/netsampler/goflow2/decoders/utils"
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

		packet.Records = make([]RecordsNetFlowV5, int(packet.Count))
		for i := 0; i < int(packet.Count) && payload.Len() >= 48; i++ {
			record := RecordsNetFlowV5{}
			err := utils.BinaryDecoder(payload, &record)
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

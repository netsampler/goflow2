package netflowlegacy

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/netsampler/goflow2/v2/decoders/utils"
)

const (
	netflowV5HeaderLen = 24
	netflowV5RecordLen = 48
)

func (p *PacketNetFlowV5) MarshalBinary() ([]byte, error) {
	return EncodeMessage(p)
}

func EncodeMessage(packet *PacketNetFlowV5) ([]byte, error) {
	if packet == nil {
		return nil, errors.New("netflowlegacy: nil packet")
	}

	version := packet.Version
	if version == 0 {
		version = 5
	}
	if version != 5 {
		return nil, fmt.Errorf("netflowlegacy: unsupported version %d", version)
	}

	count := packet.Count
	if count == 0 {
		count = uint16(len(packet.Records))
	}
	if int(count) != len(packet.Records) {
		return nil, fmt.Errorf("netflowlegacy: count mismatch header:%d records:%d", count, len(packet.Records))
	}

	totalLen := netflowV5HeaderLen + (netflowV5RecordLen * len(packet.Records))
	buf := bytes.NewBuffer(make([]byte, 0, totalLen))

	if err := utils.WriteU16(buf, version); err != nil {
		return nil, err
	}
	if err := utils.WriteU16(buf, count); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.SysUptime); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.UnixSecs); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.UnixNSecs); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.FlowSequence); err != nil {
		return nil, err
	}
	if err := utils.WriteU8(buf, packet.EngineType); err != nil {
		return nil, err
	}
	if err := utils.WriteU8(buf, packet.EngineId); err != nil {
		return nil, err
	}
	if err := utils.WriteU16(buf, packet.SamplingInterval); err != nil {
		return nil, err
	}

	for _, record := range packet.Records {
		if err := utils.WriteU32(buf, uint32(record.SrcAddr)); err != nil {
			return nil, err
		}
		if err := utils.WriteU32(buf, uint32(record.DstAddr)); err != nil {
			return nil, err
		}
		if err := utils.WriteU32(buf, uint32(record.NextHop)); err != nil {
			return nil, err
		}
		if err := utils.WriteU16(buf, record.Input); err != nil {
			return nil, err
		}
		if err := utils.WriteU16(buf, record.Output); err != nil {
			return nil, err
		}
		if err := utils.WriteU32(buf, record.DPkts); err != nil {
			return nil, err
		}
		if err := utils.WriteU32(buf, record.DOctets); err != nil {
			return nil, err
		}
		if err := utils.WriteU32(buf, record.First); err != nil {
			return nil, err
		}
		if err := utils.WriteU32(buf, record.Last); err != nil {
			return nil, err
		}
		if err := utils.WriteU16(buf, record.SrcPort); err != nil {
			return nil, err
		}
		if err := utils.WriteU16(buf, record.DstPort); err != nil {
			return nil, err
		}
		if err := utils.WriteU8(buf, record.Pad1); err != nil {
			return nil, err
		}
		if err := utils.WriteU8(buf, record.TCPFlags); err != nil {
			return nil, err
		}
		if err := utils.WriteU8(buf, record.Proto); err != nil {
			return nil, err
		}
		if err := utils.WriteU8(buf, record.Tos); err != nil {
			return nil, err
		}
		if err := utils.WriteU16(buf, record.SrcAS); err != nil {
			return nil, err
		}
		if err := utils.WriteU16(buf, record.DstAS); err != nil {
			return nil, err
		}
		if err := utils.WriteU8(buf, record.SrcMask); err != nil {
			return nil, err
		}
		if err := utils.WriteU8(buf, record.DstMask); err != nil {
			return nil, err
		}
		if err := utils.WriteU16(buf, record.Pad2); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

package netflowlegacy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
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

	var b [4]byte
	binary.BigEndian.PutUint16(b[:2], version)
	if _, err := buf.Write(b[:2]); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(b[:2], count)
	if _, err := buf.Write(b[:2]); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint32(b[:4], packet.SysUptime)
	if _, err := buf.Write(b[:4]); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint32(b[:4], packet.UnixSecs)
	if _, err := buf.Write(b[:4]); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint32(b[:4], packet.UnixNSecs)
	if _, err := buf.Write(b[:4]); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint32(b[:4], packet.FlowSequence)
	if _, err := buf.Write(b[:4]); err != nil {
		return nil, err
	}
	if err := buf.WriteByte(packet.EngineType); err != nil {
		return nil, err
	}
	if err := buf.WriteByte(packet.EngineId); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(b[:2], packet.SamplingInterval)
	if _, err := buf.Write(b[:2]); err != nil {
		return nil, err
	}

	for _, record := range packet.Records {
		binary.BigEndian.PutUint32(b[:4], uint32(record.SrcAddr))
		if _, err := buf.Write(b[:4]); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint32(b[:4], uint32(record.DstAddr))
		if _, err := buf.Write(b[:4]); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint32(b[:4], uint32(record.NextHop))
		if _, err := buf.Write(b[:4]); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint16(b[:2], record.Input)
		if _, err := buf.Write(b[:2]); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint16(b[:2], record.Output)
		if _, err := buf.Write(b[:2]); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint32(b[:4], record.DPkts)
		if _, err := buf.Write(b[:4]); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint32(b[:4], record.DOctets)
		if _, err := buf.Write(b[:4]); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint32(b[:4], record.First)
		if _, err := buf.Write(b[:4]); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint32(b[:4], record.Last)
		if _, err := buf.Write(b[:4]); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint16(b[:2], record.SrcPort)
		if _, err := buf.Write(b[:2]); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint16(b[:2], record.DstPort)
		if _, err := buf.Write(b[:2]); err != nil {
			return nil, err
		}
		if err := buf.WriteByte(record.Pad1); err != nil {
			return nil, err
		}
		if err := buf.WriteByte(record.TCPFlags); err != nil {
			return nil, err
		}
		if err := buf.WriteByte(record.Proto); err != nil {
			return nil, err
		}
		if err := buf.WriteByte(record.Tos); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint16(b[:2], record.SrcAS)
		if _, err := buf.Write(b[:2]); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint16(b[:2], record.DstAS)
		if _, err := buf.Write(b[:2]); err != nil {
			return nil, err
		}
		if err := buf.WriteByte(record.SrcMask); err != nil {
			return nil, err
		}
		if err := buf.WriteByte(record.DstMask); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint16(b[:2], record.Pad2)
		if _, err := buf.Write(b[:2]); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

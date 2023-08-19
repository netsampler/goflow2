package netflowlegacy

import (
	"bytes"
	"fmt"

	"github.com/netsampler/goflow2/v2/decoders/utils"
)

type DecoderError struct {
	Err error
}

func (e *DecoderError) Error() string {
	return fmt.Sprintf("NetFlowLegacy %s", e.Err.Error())
}

func (e *DecoderError) Unwrap() error {
	return e.Err
}

func DecodeMessageVersion(payload *bytes.Buffer, packet *PacketNetFlowV5) error {
	var version uint16
	if err := utils.BinaryDecoder(payload, &version); err != nil {
		return err
	}
	packet.Version = version
	if packet.Version != 5 {
		return &DecoderError{fmt.Errorf("unknown version %d", version)}
	}
	return DecodeMessage(payload, packet)
}

func DecodeMessage(payload *bytes.Buffer, packet *PacketNetFlowV5) error {
	if err := utils.BinaryDecoder(payload,
		&packet.Count,
		&packet.SysUptime,
		&packet.UnixSecs,
		&packet.UnixNSecs,
		&packet.FlowSequence,
		&packet.EngineType,
		&packet.EngineId,
		&packet.SamplingInterval,
	); err != nil {
		return &DecoderError{err}
	}

	packet.Records = make([]RecordsNetFlowV5, int(packet.Count)) // maximum is 65535 which would be 3MB
	for i := 0; i < int(packet.Count) && payload.Len() >= 48; i++ {
		record := RecordsNetFlowV5{}
		var srcAddr, dstAddr, nextHop uint32

		if err := utils.BinaryDecoder(payload,
			&srcAddr,
			&dstAddr,
			&nextHop,
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
		); err != nil {
			return &DecoderError{err}
		}
		record.SrcAddr = IPAddress(srcAddr)
		record.DstAddr = IPAddress(dstAddr)
		record.NextHop = IPAddress(nextHop)
		packet.Records[i] = record
	}

	return nil
}

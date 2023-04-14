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

type NetFlowLegacyDecoderError struct {
	Version     uint16
	Type        string
	ObsDomainId uint32
	TemplateId  uint16
	Err         error
}

func (e *NetFlowLegacyDecoderError) Error() string {
	return fmt.Sprintf("NetFlow Legacy decoder error: [version:%d type:%s obsDomainId:%v: templateId:%d] %s", e.Version, e.Type, e.ObsDomainId, e.TemplateId, e.Err.Error())
}

func (e *NetFlowLegacyDecoderError) Unwrap() error {
	return e.Err
}

func DecodeMessageVersion(payload *bytes.Buffer, packet *PacketNetFlowV5) error {
	var version uint16
	if err := utils.BinaryDecoder(payload, &version); err != nil {
		return err
	}
	packet.Version = version
	if packet.Version != 5 {
		return NewErrorVersion(version)
	}
	return DecodeMessage(payload, packet)
}

func DecodeMessage(payload *bytes.Buffer, packet *PacketNetFlowV5) error {
	//packet := PacketNetFlowV5{}
	//if packet.Version == 5 {

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
		if err := utils.BinaryDecoder(payload, &record); err != nil {
			return err
		}
		packet.Records[i] = record
	}

	return nil
	/*} else {
		return nil, NewErrorVersion(version)
	}*/
}

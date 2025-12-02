package protoproducer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
	"github.com/netsampler/goflow2/v2/decoders/utils"
)

type SamplingRateOptions struct {
	PacketSpace      uint32
	PacketInterval   uint32
	RandomInterval   uint32
	SamplingInterval uint32
}

func SamplingFromOptions(records netflow.OptionsDataRecord) (*SamplingRateOptions, error) {
	samplingOptions := SamplingRateOptions{}
	for _, option := range records.OptionsValues {
		switch option.Type {
		case netflow.IPFIX_FIELD_samplingPacketSpace:
			if err := binDecode(option.Value, &samplingOptions.PacketSpace); err != nil {
				return nil, fmt.Errorf("can't make samplingPacketSpace from option value %b", option.Value)
			}
		case netflow.IPFIX_FIELD_samplingPacketInterval:
			if err := binDecode(option.Value, &samplingOptions.PacketInterval); err != nil {
				return nil, fmt.Errorf("can't make samplingPacketInterval from option value %b", option.Value)
			}
		case netflow.IPFIX_FIELD_samplerRandomInterval:
			if err := binDecode(option.Value, &samplingOptions.RandomInterval); err != nil {
				return nil, fmt.Errorf("can't make samplerRandomInterval from option value %b", option.Value)
			}
		case netflow.IPFIX_FIELD_samplingInterval:
			if err := binDecode(option.Value, &samplingOptions.SamplingInterval); err != nil {
				return nil, fmt.Errorf("can't make samplingInterval from option value %b", option.Value)
			}
		default:
		}
	}
	if samplingOptions.SamplingRate() > 0 {
		return &samplingOptions, nil
	}
	return nil, errors.New("no sampling rate options found")
}

func (s *SamplingRateOptions) SamplingRate() uint32 {
	var tmpRate uint32
	if s.PacketInterval > 0 {
		tmpRate += s.PacketInterval
		if s.PacketSpace > 0 {
			tmpRate += s.PacketSpace
		}
		return tmpRate
	}
	if s.RandomInterval > 0 {
		return s.RandomInterval
	}
	if s.SamplingInterval > 0 {
		return s.SamplingInterval
	}
	return 0
}

func binDecode(source any, destination any) error {
	if source == nil {
		return errors.New("can't decode nil source")
	}
	if destination == nil {
		return errors.New("can't decode to nil destination")
	}
	valueBytes, ok := source.([]byte)
	if !ok {
		return errors.New("source value is not []byte")
	}
	if err := utils.BinaryRead(bytes.NewBuffer(valueBytes), binary.BigEndian, destination); err != nil {
		return err
	}
	return nil
}

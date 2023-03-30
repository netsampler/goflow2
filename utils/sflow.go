package utils

import (
	"bytes"
	"fmt"

	"github.com/netsampler/goflow2/decoders/sflow"
	"github.com/netsampler/goflow2/format"
	"github.com/netsampler/goflow2/producer"
	"github.com/netsampler/goflow2/transport"
)

type StateSFlow struct {
	stopper

	Format    format.FormatInterface
	Transport transport.TransportInterface
	Producer  producer.ProducerInterface

	Logger Logger

	Config       *producer.ProducerConfig
	configMapped *producer.ProducerConfigMapped
}

func NewSFlowDecoder() *StateSFlow {
	s := &StateSFlow{}
	return s
}

func (s *StateSFlow) DecodeFlow(msg interface{}) error {
	pkt, ok := msg.(*Message)
	if !ok {
		return fmt.Errorf("flow is not *Message")
	}
	buf := bytes.NewBuffer(pkt.Payload)
	//key := pkt.Src.String()

	ts := uint64(pkt.Received.Unix())

	var packet sflow.Packet
	if err := sflow.DecodeMessageVersion(buf, &packet); err != nil {
		return err
	}

	args := producer.ProcessArgs{
		Config: s.configMapped,
	}
	if s.Producer == nil {
		return nil
	}
	flowMessageSet, err := s.Producer(&packet, &args)
	if err != nil {
		return err
	}

	for _, fmsg := range flowMessageSet {
		fmsg.TimeReceived = ts
		fmsg.TimeFlowStart = ts
		fmsg.TimeFlowEnd = ts

		if s.Format != nil {
			key, data, err := s.Format.Format(fmsg)

			if err != nil && s.Logger != nil {
				s.Logger.Error(err)
			}
			if err == nil && s.Transport != nil {
				err = s.Transport.Send(key, data)
				if err != nil {
					s.Logger.Error(err)
				}
			}
		}
	}

	return nil
}

func (s *StateSFlow) initConfig() {
	s.configMapped = producer.NewProducerConfigMapped(s.Config)
}

func (s *StateSFlow) InitConfig(config *producer.ProducerConfig) {
	s.configMapped = producer.NewProducerConfigMapped(config)
}

func (s *StateSFlow) FlowRoutine(workers int, addr string, port int, reuseport bool) error {
	if err := s.start(); err != nil {
		return err
	}
	s.initConfig()
	return UDPStoppableRoutine(s.stopCh, "sFlow", s.DecodeFlow, workers, addr, port, reuseport, s.Logger)
}

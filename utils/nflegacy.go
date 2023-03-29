package utils

import (
	"bytes"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/netsampler/goflow2/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/format"
	flowmessage "github.com/netsampler/goflow2/pb"
	"github.com/netsampler/goflow2/producer"
	"github.com/netsampler/goflow2/tmpmetrics" // temporary
	"github.com/netsampler/goflow2/transport"
)

type StateNFLegacy struct {
	stopper

	Format    format.FormatInterface
	Transport transport.TransportInterface
	Logger    Logger
}

func (s *StateNFLegacy) DecodeFlow(msg interface{}) error {
	pkt, ok := msg.(*Message)
	if !ok {
		return fmt.Errorf("flow is not *Message")
	}
	buf := bytes.NewBuffer(pkt.Payload)
	key := pkt.Src.String()
	addr := pkt.Src.Addr().As16()
	samplerAddress := addr[:]

	ts := uint64(pkt.Received.Unix())

	timeTrackStart := time.Now()

	var packet netflowlegacy.PacketNetFlowV5
	err := netflowlegacy.DecodeMessageVersion(buf, &packet)

	if err != nil {
		switch err.(type) {
		case *netflowlegacy.ErrorVersion:
			metrics.NetFlowErrors.With(
				prometheus.Labels{
					"router": key,
					"error":  "error_version",
				}).
				Inc()
		}
		return err
	}

	metrics.NetFlowStats.With(
		prometheus.Labels{
			"router":  key,
			"version": "5",
		}).
		Inc()
	metrics.NetFlowSetStatsSum.With(
		prometheus.Labels{
			"router":  key,
			"version": "5",
			"type":    "DataFlowSet",
		}).
		Add(float64(packet.Count))

	var flowMessageSet []*flowmessage.FlowMessage
	flowMessageSet, err = producer.ProcessMessageNetFlowLegacy(packet)

	timeTrackStop := time.Now()
	metrics.DecoderTime.With(
		prometheus.Labels{
			"name": "NetFlowV5",
		}).
		Observe(float64((timeTrackStop.Sub(timeTrackStart)).Nanoseconds()) / 1000)

	for _, fmsg := range flowMessageSet {
		fmsg.TimeReceived = ts
		fmsg.SamplerAddress = samplerAddress

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

func (s *StateNFLegacy) FlowRoutine(workers int, addr string, port int, reuseport bool) error {
	if err := s.start(); err != nil {
		return err
	}
	return UDPStoppableRoutine(s.stopCh, "NetFlowV5", s.DecodeFlow, workers, addr, port, reuseport, s.Logger)
}

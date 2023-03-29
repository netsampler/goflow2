package utils

import (
	"bytes"
	"fmt"
	"net"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/netsampler/goflow2/decoders/sflow"
	"github.com/netsampler/goflow2/format"
	"github.com/netsampler/goflow2/producer"
	"github.com/netsampler/goflow2/tmpmetrics" // temporary
	"github.com/netsampler/goflow2/transport"
)

type StateSFlow struct {
	stopper

	Format    format.FormatInterface
	Transport transport.TransportInterface
	Logger    Logger
	Producer  interface{}

	Config       *producer.ProducerConfig
	configMapped *producer.ProducerConfigMapped
}

func (s *StateSFlow) DecodeFlow(msg interface{}) error {
	pkt, ok := msg.(*Message)
	if !ok {
		return fmt.Errorf("flow is not *Message")
	}
	buf := bytes.NewBuffer(pkt.Payload)
	key := pkt.Src.String()

	ts := uint64(pkt.Received.Unix())

	timeTrackStart := time.Now()

	var packet sflow.Packet
	if err := sflow.DecodeMessageVersion(buf, &packet); err != nil {
		return err
	}

	agentStr := net.IP(packet.AgentIP).String()
	metrics.SFlowStats.With(
		prometheus.Labels{
			"router":  key,
			"agent":   agentStr,
			"version": "5",
		}).
		Inc()

	for _, samples := range packet.Samples {
		typeStr := "unknown"
		countRec := 0
		switch samplesConv := samples.(type) {
		case sflow.FlowSample:
			typeStr = "FlowSample"
			countRec = len(samplesConv.Records)
		case sflow.CounterSample:
			typeStr = "CounterSample"
			if samplesConv.Header.Format == 4 {
				typeStr = "Expanded" + typeStr
			}
			countRec = len(samplesConv.Records)
		case sflow.ExpandedFlowSample:
			typeStr = "ExpandedFlowSample"
			countRec = len(samplesConv.Records)
		}
		metrics.SFlowSampleStatsSum.With(
			prometheus.Labels{
				"router":  key,
				"agent":   agentStr,
				"version": "5",
				"type":    typeStr,
			}).
			Inc()

		metrics.SFlowSampleRecordsStatsSum.With(
			prometheus.Labels{
				"router":  key,
				"agent":   agentStr,
				"version": "5",
				"type":    typeStr,
			}).
			Add(float64(countRec))

	}

	flowMessageSet, err := producer.ProcessMessage(&packet, nil, s.configMapped)
	if err != nil {
		return err
	}

	timeTrackStop := time.Now()
	metrics.DecoderTime.With(
		prometheus.Labels{
			"name": "sFlow",
		}).
		Observe(float64((timeTrackStop.Sub(timeTrackStart)).Nanoseconds()) / 1000)

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

func (s *StateSFlow) FlowRoutine(workers int, addr string, port int, reuseport bool) error {
	if err := s.start(); err != nil {
		return err
	}
	s.initConfig()
	return UDPStoppableRoutine(s.stopCh, "sFlow", s.DecodeFlow, workers, addr, port, reuseport, s.Logger)
}

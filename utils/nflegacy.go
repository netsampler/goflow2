package utils

import (
	"bytes"
	"strconv"
	"time"

	"github.com/netsampler/goflow2/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/format"
	flowmessage "github.com/netsampler/goflow2/pb"
	"github.com/netsampler/goflow2/producer"
	"github.com/netsampler/goflow2/transport"
	"github.com/prometheus/client_golang/prometheus"
)

var MaxNegativeFlowsSequenceDifference = 1000

type StateNFLegacy struct {
	stopper

	Format    format.FormatInterface
	Transport transport.TransportInterface
	Logger    Logger

	missingFlowsTracker *MissingFlowsTracker
}

func NewStateNFLegacy() *StateNFLegacy {
	return &StateNFLegacy{}
}

func (s *StateNFLegacy) DecodeFlow(msg interface{}) error {
	pkt := msg.(BaseMessage)
	buf := bytes.NewBuffer(pkt.Payload)
	key := pkt.Src.String()
	samplerAddress := pkt.Src
	if samplerAddress.To4() != nil {
		samplerAddress = samplerAddress.To4()
	}

	ts := uint64(time.Now().UTC().Unix())
	if pkt.SetTime {
		ts = uint64(pkt.RecvTime.UTC().Unix())
	}

	timeTrackStart := time.Now()
	msgDec, err := netflowlegacy.DecodeMessage(buf)

	if err != nil {
		switch err.(type) {
		case *netflowlegacy.ErrorVersion:
			NetFlowErrors.With(
				prometheus.Labels{
					"router": key,
					"error":  "error_version",
				}).
				Inc()
		}
		return err
	}

	switch msgDecConv := msgDec.(type) {
	case netflowlegacy.PacketNetFlowV5:
		NetFlowStats.With(
			prometheus.Labels{
				"router":  key,
				"version": "5",
			}).
			Inc()
		NetFlowSetStatsSum.With(
			prometheus.Labels{
				"router":  key,
				"version": "5",
				"type":    "DataFlowSet",
			}).
			Add(float64(msgDecConv.Count))

		engineType := strconv.Itoa(int(msgDecConv.EngineType))
		engineId := strconv.Itoa(int(msgDecConv.EngineId))
		missingFlowsKey := key + "|" + engineType + "|" + engineId
		missingFlows := s.missingFlowsTracker.countMissing(missingFlowsKey, msgDecConv.FlowSequence, msgDecConv.Count)

		NetFlowFlowsMissing.With(
			prometheus.Labels{
				"router":      key,
				"version":     "5",
				"engine_id":   engineId,
				"engine_type": engineType,
			}).
			Set(float64(missingFlows))
		NetFlowFlowsSequence.With(
			prometheus.Labels{
				"router":      key,
				"version":     "5",
				"engine_id":   engineId,
				"engine_type": engineType,
			}).
			Set(float64(msgDecConv.FlowSequence))
	}

	var flowMessageSet []*flowmessage.FlowMessage
	flowMessageSet, err = producer.ProcessMessageNetFlowLegacy(msgDec)

	timeTrackStop := time.Now()
	DecoderTime.With(
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

func (s *StateNFLegacy) initConfig() {
	s.missingFlowsTracker = NewMissingFlowsTracker(MaxNegativeFlowsSequenceDifference)
}

func (s *StateNFLegacy) FlowRoutine(workers int, addr string, port int, reuseport bool) error {
	if err := s.start(); err != nil {
		return err
	}
	s.initConfig()
	return UDPStoppableRoutine(s.stopCh, "NetFlowV5", s.DecodeFlow, workers, addr, port, reuseport, s.Logger)
}

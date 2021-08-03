package utils

import (
	"bytes"
	"net"
	"time"
	"fmt"

	"github.com/netsampler/goflow2/decoders/sflow"
	"github.com/netsampler/goflow2/format"
	flowmessage "github.com/netsampler/goflow2/pb"
	"github.com/netsampler/goflow2/producer"
	"github.com/netsampler/goflow2/transport"
	"github.com/prometheus/client_golang/prometheus"
)

type StateSFlow struct {
	Format    format.FormatInterface
	Transport transport.TransportInterface
	Logger    Logger

	Config *producer.SFlowProducerConfig
}

func (s *StateSFlow) DecodeFlow(msg interface{}) error {
	pkt := msg.(BaseMessage)
	buf := bytes.NewBuffer(pkt.Payload)
	key := pkt.Src.String()

	ts := uint64(time.Now().UTC().Unix())
	if pkt.SetTime {
		ts = uint64(pkt.RecvTime.UTC().Unix())
	}

	timeTrackStart := time.Now()
	msgDec, err := sflow.DecodeMessage(buf)

	if err != nil {
		switch err.(type) {
		case *sflow.ErrorVersion:
			SFlowErrors.With(
				prometheus.Labels{
					"router": key,
					"error":  "error_version",
				}).
				Inc()
		case *sflow.ErrorIPVersion:
			SFlowErrors.With(
				prometheus.Labels{
					"router": key,
					"error":  "error_ip_version",
				}).
				Inc()
		case *sflow.ErrorDataFormat:
			SFlowErrors.With(
				prometheus.Labels{
					"router": key,
					"error":  "error_data_format",
				}).
				Inc()
		default:
			SFlowErrors.With(
				prometheus.Labels{
					"router": key,
					"error":  "error_decoding",
				}).
				Inc()
		}
		return err
	}

	switch msgDecConv := msgDec.(type) {
	case sflow.Packet:
		agentStr := net.IP(msgDecConv.AgentIP).String()
		SFlowStats.With(
			prometheus.Labels{
				"router":  key,
				"agent":   agentStr,
				"version": "5",
			}).
			Inc()

		for _, samples := range msgDecConv.Samples {
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

				for _, records := range samplesConv.Records{
					switch dataConv := records.Data.(type) {
						case sflow.IfCounters:
							SFlowIfSpeed.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfSpeed))
							// bit 0 = ifAdminStatus (0 = down, 1 = up)
							ifAdminStatus := float64(0)
							if dataConv.IfStatus & 1 == 1  {
								ifAdminStatus = 1
							}
							SFlowIfAdminStatus.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(ifAdminStatus)
							// bit 1 = ifOperStatus (0 = down, 1 = up)
							ifOperStatus := float64(0)
							if dataConv.IfStatus & 2 == 2  {
								ifOperStatus = 1
							}
							SFlowIfOperStatus.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(ifOperStatus)
							SFlowIfDirection.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfDirection))
							SFlowIfInOctets.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfInOctets))
							SFlowIfInUcastPkts.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfInUcastPkts))
							SFlowIfInMulticastPkts.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfInMulticastPkts))
							SFlowIfInBroadcastPkts.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfInBroadcastPkts))
							SFlowIfInDiscards.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfInDiscards))
							SFlowIfInErrors.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfInErrors))
							SFlowIfInUnknownProtos.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfInUnknownProtos))
							SFlowIfOutOctets.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfOutOctets))
							SFlowIfOutUcastPkts.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfOutUcastPkts))
							SFlowIfOutMulticastPkts.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfOutMulticastPkts))
							SFlowIfOutBroadcastPkts.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfOutBroadcastPkts))
							SFlowIfOutDiscards.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfOutDiscards))
							SFlowIfOutErrors.With(
								prometheus.Labels{
									"router":  key,
									"agent":   agentStr,
									"version": "5",
									"type":    typeStr,
									"ifindex": fmt.Sprint(dataConv.IfIndex),
									"iftype": fmt.Sprint(dataConv.IfType),
								}).
								Set(float64(dataConv.IfOutErrors))
					}
				}
			case sflow.ExpandedFlowSample:
				typeStr = "ExpandedFlowSample"
				countRec = len(samplesConv.Records)
			}
			SFlowSampleStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"agent":   agentStr,
					"version": "5",
					"type":    typeStr,
				}).
				Inc()

			SFlowSampleRecordsStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"agent":   agentStr,
					"version": "5",
					"type":    typeStr,
				}).
				Add(float64(countRec))
		}

	}

	var flowMessageSet []*flowmessage.FlowMessage
	flowMessageSet, err = producer.ProcessMessageSFlowConfig(msgDec, s.Config)

	timeTrackStop := time.Now()
	DecoderTime.With(
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
				s.Transport.Send(key, data)
			}
		}
	}

	return nil
}

func (s *StateSFlow) FlowRoutine(workers int, addr string, port int, reuseport bool) error {
	return UDPRoutine("sFlow", s.DecodeFlow, workers, addr, port, reuseport, s.Logger)
}

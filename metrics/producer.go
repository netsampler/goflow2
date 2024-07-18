package metrics

import (
	"net/netip"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
	"github.com/netsampler/goflow2/v2/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v2/decoders/sflow"
	"github.com/netsampler/goflow2/v2/producer"
	"github.com/netsampler/goflow2/v2/producer/proto"

	"github.com/prometheus/client_golang/prometheus"
)

type PromProducerWrapper struct {
	wrapped producer.ProducerInterface
}

func (p *PromProducerWrapper) Produce(msg interface{}, args *producer.ProduceArgs) ([]producer.ProducerMessage, error) {
	flowMessageSet, err := p.wrapped.Produce(msg, args)
	if err != nil {
		return flowMessageSet, err
	}
	key := args.Src.Addr().Unmap().String()
	var nfvariant bool
	var versionStr string
	switch packet := msg.(type) {
	case *sflow.Packet:
		agentStr := "unk"
		agentIp, ok := netip.AddrFromSlice(packet.AgentIP)
		if ok {
			agentStr = agentIp.String()
		}

		SFlowStats.With(
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

	case *netflowlegacy.PacketNetFlowV5:
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
			Add(float64(packet.Count))

	case *netflow.NFv9Packet:
		NetFlowStats.With(
			prometheus.Labels{
				"router":  key,
				"version": "9",
			}).
			Inc()
		recordCommonNetFlowMetrics(9, key, packet.FlowSets)
		nfvariant = true
		versionStr = "9"

	case *netflow.IPFIXPacket:
		NetFlowStats.With(
			prometheus.Labels{
				"router":  key,
				"version": "10",
			}).
			Inc()
		recordCommonNetFlowMetrics(10, key, packet.FlowSets)
		nfvariant = true
		versionStr = "10"
	}

	if nfvariant {
		for _, msg := range flowMessageSet {
			fmsg, ok := msg.(*protoproducer.ProtoProducerMessage)
			if !ok {
				continue
			}
			timeDiff := fmsg.TimeReceivedNs - fmsg.TimeFlowEndNs

			NetFlowTimeStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": versionStr,
				}).
				Observe(float64(timeDiff) / 1e9)
		}
	}

	return flowMessageSet, err
}

func (p *PromProducerWrapper) Close() {
	p.wrapped.Close()
}

func (p *PromProducerWrapper) Commit(flowMessageSet []producer.ProducerMessage) {
	p.wrapped.Commit(flowMessageSet)
}

// Wraps a producer with metrics
func WrapPromProducer(wrapped producer.ProducerInterface) producer.ProducerInterface {
	return &PromProducerWrapper{
		wrapped: wrapped,
	}
}

// metrics template system

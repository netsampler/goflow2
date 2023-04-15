package protoproducer

import (
	"fmt"

	"github.com/netsampler/goflow2/decoders/netflow"
	"github.com/netsampler/goflow2/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/decoders/sflow"
	"github.com/netsampler/goflow2/producer"
)

type ProtoProducer struct {
	cfgMapped          *producerConfigMapped
	samplingRateSystem SamplingRateSystem
}

func (p *ProtoProducer) enrich(flowMessageSet []producer.ProducerMessage, cb func(msg *ProtoProducerMessage)) {
	for _, msg := range flowMessageSet {
		fmsg, ok := msg.(*ProtoProducerMessage)
		if !ok {
			continue
		}
		cb(fmsg)
	}
}

func (p *ProtoProducer) Produce(msg interface{}, args *producer.ProduceArgs) (flowMessageSet []producer.ProducerMessage, err error) {
	tr := uint64(args.TimeReceived.UnixNano())
	sa, _ := args.SamplerAddress.MarshalBinary()
	switch msgConv := msg.(type) {
	case *netflowlegacy.PacketNetFlowV5: //todo: rename PacketNetFlowV5
		flowMessageSet, err = ProcessMessageNetFlowLegacy(msgConv)

		p.enrich(flowMessageSet, func(fmsg *ProtoProducerMessage) {
			fmsg.SamplerAddress = sa
		})
	case *netflow.NFv9Packet:
		flowMessageSet, err = ProcessMessageNetFlowV9Config(msgConv, p.samplingRateSystem, p.cfgMapped)

		p.enrich(flowMessageSet, func(fmsg *ProtoProducerMessage) {
			fmsg.TimeReceivedNs = tr
			fmsg.SamplerAddress = sa
		})
	case *netflow.IPFIXPacket:
		flowMessageSet, err = ProcessMessageIPFIXConfig(msgConv, p.samplingRateSystem, p.cfgMapped)

		p.enrich(flowMessageSet, func(fmsg *ProtoProducerMessage) {
			fmsg.TimeReceivedNs = tr
			fmsg.SamplerAddress = sa
		})
	case *sflow.Packet:
		flowMessageSet, err = ProcessMessageSFlowConfig(msgConv, p.cfgMapped)

		p.enrich(flowMessageSet, func(fmsg *ProtoProducerMessage) {
			fmsg.TimeReceivedNs = tr
			fmsg.TimeFlowStartNs = tr
			fmsg.TimeFlowEndNs = tr
		})
	default:
		return flowMessageSet, fmt.Errorf("flow not recognized")
	}

	p.enrich(flowMessageSet, func(fmsg *ProtoProducerMessage) {
		fmsg.formatter = p.cfgMapped.Formatter
	})
	return flowMessageSet, err
}

func (p *ProtoProducer) Commit(flowMessageSet []producer.ProducerMessage) {
	for _, fmsg := range flowMessageSet {
		protoMessagePool.Put(fmsg)
	}
}

func (p *ProtoProducer) Close() {}

func CreateProtoProducerWithConfig(cfg *ProducerConfig, samplingRateSystem SamplingRateSystem) (producer.ProducerInterface, error) {
	cfgMapped, err := mapConfig(cfg)
	return &ProtoProducer{
		cfgMapped:          cfgMapped,
		samplingRateSystem: samplingRateSystem,
	}, err
}

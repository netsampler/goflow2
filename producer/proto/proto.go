package protoproducer

import (
	"fmt"
	"sync"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
	"github.com/netsampler/goflow2/v2/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v2/decoders/sflow"
	"github.com/netsampler/goflow2/v2/producer"
)

type ProtoProducer struct {
	cfgMapped          *producerConfigMapped
	samplinglock       *sync.RWMutex
	sampling           map[string]SamplingRateSystem
	samplingRateSystem func() SamplingRateSystem
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

func (p *ProtoProducer) getSamplingRateSystem(args *producer.ProduceArgs) SamplingRateSystem {
	key := args.Src.Addr().String()
	p.samplinglock.RLock()
	sampling, ok := p.sampling[key]
	p.samplinglock.RUnlock()
	if !ok {
		sampling = p.samplingRateSystem()
		p.samplinglock.Lock()
		p.sampling[key] = sampling
		p.samplinglock.Unlock()
	}

	return sampling
}

func (p *ProtoProducer) Produce(msg interface{}, args *producer.ProduceArgs) (flowMessageSet []producer.ProducerMessage, err error) {
	tr := uint64(args.TimeReceived.UnixNano())
	sa, _ := args.SamplerAddress.Unmap().MarshalBinary()
	switch msgConv := msg.(type) {
	case *netflowlegacy.PacketNetFlowV5:
		flowMessageSet, err = ProcessMessageNetFlowLegacy(msgConv)

		p.enrich(flowMessageSet, func(fmsg *ProtoProducerMessage) {
			fmsg.TimeReceivedNs = tr
			fmsg.SamplerAddress = sa
		})
	case *netflow.NFv9Packet:
		samplingRateSystem := p.getSamplingRateSystem(args)
		flowMessageSet, err = ProcessMessageNetFlowV9Config(msgConv, samplingRateSystem, p.cfgMapped)

		p.enrich(flowMessageSet, func(fmsg *ProtoProducerMessage) {
			fmsg.TimeReceivedNs = tr
			fmsg.SamplerAddress = sa
		})
	case *netflow.IPFIXPacket:
		samplingRateSystem := p.getSamplingRateSystem(args)
		flowMessageSet, err = ProcessMessageIPFIXConfig(msgConv, samplingRateSystem, p.cfgMapped)

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

func CreateProtoProducer(cfg *ProducerConfig, samplingRateSystem func() SamplingRateSystem) (producer.ProducerInterface, error) {
	cfgMapped, err := mapConfig(cfg)
	return &ProtoProducer{
		cfgMapped:          cfgMapped,
		samplinglock:       &sync.RWMutex{},
		sampling:           make(map[string]SamplingRateSystem),
		samplingRateSystem: samplingRateSystem,
	}, err
}

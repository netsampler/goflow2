package producer

import (
	"fmt"
	"net/netip"

	"github.com/netsampler/goflow2/decoders/netflow"
	"github.com/netsampler/goflow2/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/decoders/sflow"
	flowmessage "github.com/netsampler/goflow2/pb"
)

// Interface of the messages
// Passed to the Formatter
// Can be protobuf
type ProducerMessage interface {
}

type ProducerInterface interface {
	Produce(msg interface{}, args *ProduceArgs) ([]*flowmessage.FlowMessage, error)
	Close()
}

type ProduceArgs struct {
	MessageFactory     interface{}
	SamplingRateSystem SamplingRateSystem

	Src netip.AddrPort
	Dst netip.AddrPort
}

type ProtoProducer struct {
	cfgMapped *producerConfigMapped
}

func (p *ProtoProducer) Produce(msg interface{}, args *ProduceArgs) ([]*flowmessage.FlowMessage, error) {
	switch msgConv := msg.(type) {
	case *netflowlegacy.PacketNetFlowV5: //todo: rename PacketNetFlowV5
		return ProcessMessageNetFlowLegacy(msgConv)
	case *netflow.NFv9Packet:
		return ProcessMessageNetFlowV9Config(msgConv, args.SamplingRateSystem, p.cfgMapped)
	case *netflow.IPFIXPacket:
		return ProcessMessageIPFIXConfig(msgConv, args.SamplingRateSystem, p.cfgMapped)
	case *sflow.Packet:
		return ProcessMessageSFlowConfig(msgConv, p.cfgMapped)
	default:
		return nil, fmt.Errorf("flow not recognized")
	}
}

func (p *ProtoProducer) Close() {}

func CreateProducerWithConfig(cfg *ProducerConfig) ProducerInterface {
	return &ProtoProducer{
		cfgMapped: mapConfig(cfg),
	}
}

// Producer that keeps the same format
// as the original flow samples.
// This can be used for debugging (eg: getting NetFlow Option Templates)
type RawProducer struct {
}

func (p *RawProducer) Produce(msg interface{}, args *ProduceArgs) ([]*flowmessage.FlowMessage, error) {
	// should return msg wrapped
	// []*interface{msg,}
	return nil, nil
}

func (p *RawProducer) Close() {}

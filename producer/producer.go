package producer

import (
	"fmt"
	"net/netip"

	"github.com/netsampler/goflow2/decoders/netflow"
	"github.com/netsampler/goflow2/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/decoders/sflow"
	flowmessage "github.com/netsampler/goflow2/pb"
)

type ProducerInterface func(msg interface{}, args *ProcessArgs) ([]*flowmessage.FlowMessage, error)

type ProcessArgs struct {
	SamplingRateSystem SamplingRateSystem

	Src netip.AddrPort
	Dst netip.AddrPort
}

func CreateProducerWithConfig(cfg *ProducerConfig) ProducerInterface {
	cfgMapped := mapConfig(cfg)

	return func(msg interface{}, args *ProcessArgs) ([]*flowmessage.FlowMessage, error) {
		switch msgConv := msg.(type) {
		case *netflowlegacy.PacketNetFlowV5: //todo: rename PacketNetFlowV5
			return ProcessMessageNetFlowLegacy(msgConv)
		case *netflow.NFv9Packet:
			return ProcessMessageNetFlowV9Config(msgConv, args.SamplingRateSystem, cfgMapped)
		case *netflow.IPFIXPacket:
			return ProcessMessageIPFIXConfig(msgConv, args.SamplingRateSystem, cfgMapped)
		case *sflow.Packet:
			return ProcessMessageSFlowConfig(msgConv, cfgMapped)
		default:
			return nil, fmt.Errorf("flow not recognized")
		}
	}

}

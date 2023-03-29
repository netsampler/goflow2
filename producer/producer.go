package producer

import (
	"fmt"

	"github.com/netsampler/goflow2/decoders/netflow"
	"github.com/netsampler/goflow2/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/decoders/sflow"
	flowmessage "github.com/netsampler/goflow2/pb"
)

type ProducerInterface func(msg interface{}, args *ProcessArgs) ([]*flowmessage.FlowMessage, error)

type ProcessArgs struct {
	Config             *ProducerConfigMapped
	SamplingRateSystem SamplingRateSystem
}

// Converts various types of flow into a single sample format
func ProduceMessage(msg interface{}, args *ProcessArgs) ([]*flowmessage.FlowMessage, error) {
	switch msgConv := msg.(type) {
	case *netflowlegacy.PacketNetFlowV5: //todo: rename PacketNetFlowV5
		return ProcessMessageNetFlowLegacy2(msgConv)
	case *netflow.NFv9Packet:
		return ProcessMessageNetFlowV9Config(msgConv, args.SamplingRateSystem, args.Config)
	case *netflow.IPFIXPacket:
		return ProcessMessageIPFIXConfig(msgConv, args.SamplingRateSystem, args.Config)
	case *sflow.Packet:
		return ProcessMessageSFlowConfig2(msgConv, args.Config)
	default:
		return nil, fmt.Errorf("flow not recognized")
	}
}

package utils

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/netsampler/goflow2/decoders/netflow"
	"github.com/netsampler/goflow2/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/decoders/sflow"
	"github.com/netsampler/goflow2/decoders/utils"
	"github.com/netsampler/goflow2/format"
	flowmessage "github.com/netsampler/goflow2/pb"
	"github.com/netsampler/goflow2/producer"
	"github.com/netsampler/goflow2/transport"
)

type flowpipe struct {
	format    format.FormatInterface
	transport transport.TransportInterface
	producer  producer.ProducerInterface

	configMapped *producer.ProducerConfigMapped

	netFlowTemplater func() netflow.NetFlowTemplateSystem
}

type PipeConfig struct {
	Format    format.FormatInterface
	Transport transport.TransportInterface
	Producer  producer.ProducerInterface

	// Producer configuration to map additional fields
	ProducerConfig *producer.ProducerConfig

	// Function that create Template Systems
	NetFlowTemplater func() netflow.NetFlowTemplateSystem
}

func (p *flowpipe) formatSend(flowMessageSet []*flowmessage.FlowMessage) error {
	for _, fmsg := range flowMessageSet {
		if p.format != nil {
			key, data, err := p.format.Format(fmsg)
			if err != nil {
				return err
			}
			if p.transport != nil {
				if err = p.transport.Send(key, data); err != nil {
					return err
				}
			}
		}
	}
	return nil

}

func (p *flowpipe) parseConfig(cfg *PipeConfig) {
	p.format = cfg.Format
	p.transport = cfg.Transport
	p.producer = cfg.Producer
	p.configMapped = producer.NewProducerConfigMapped(cfg.ProducerConfig)
	if cfg.NetFlowTemplater != nil {
		p.netFlowTemplater = cfg.NetFlowTemplater
	} else {
		p.netFlowTemplater = netflow.CreateTemplateSystem
	}

}

type SFlowPipe struct {
	flowpipe
}

type NetFlowPipe struct {
	flowpipe

	templateslock *sync.RWMutex
	templates     map[string]netflow.NetFlowTemplateSystem

	samplinglock *sync.RWMutex
	sampling     map[string]producer.SamplingRateSystem
}

func NewSFlowPipe(cfg *PipeConfig) *SFlowPipe {
	p := &SFlowPipe{}
	p.parseConfig(cfg)
	return p
}

func (p *SFlowPipe) DecodeFlow(msg interface{}) error {
	pkt, ok := msg.(*Message)
	if !ok {
		return fmt.Errorf("flow is not *Message")
	}
	buf := bytes.NewBuffer(pkt.Payload)
	//key := pkt.Src.String()

	ts := uint64(pkt.Received.Unix())

	var packet sflow.Packet
	if err := sflow.DecodeMessageVersion(buf, &packet); err != nil {
		return err // wrap with decode
	}

	args := producer.ProcessArgs{
		Config: p.configMapped,

		Src: pkt.Src,
		Dst: pkt.Dst,
	}
	if p.producer == nil {
		return nil
	}
	flowMessageSet, err := p.producer(&packet, &args)
	if err != nil {
		return err // wrap with produce
	}

	for _, fmsg := range flowMessageSet {
		fmsg.TimeReceived = ts
		fmsg.TimeFlowStart = ts
		fmsg.TimeFlowEnd = ts
	}

	return p.formatSend(flowMessageSet)
}

func NewNetFlowPipe(cfg *PipeConfig) *NetFlowPipe {
	p := &NetFlowPipe{
		templateslock: &sync.RWMutex{},
		samplinglock:  &sync.RWMutex{},
		templates:     make(map[string]netflow.NetFlowTemplateSystem),
		sampling:      make(map[string]producer.SamplingRateSystem),
	}
	p.parseConfig(cfg)
	return p
}

func (p *NetFlowPipe) DecodeFlow(msg interface{}) error {
	pkt, ok := msg.(*Message)
	if !ok {
		return fmt.Errorf("flow is not *Message")
	}
	buf := bytes.NewBuffer(pkt.Payload)

	key := pkt.Src.String()
	addr := pkt.Src.Addr().As16()
	samplerAddress := addr[:]

	p.templateslock.RLock()
	templates, ok := p.templates[key]
	p.templateslock.RUnlock()
	if !ok {
		templates = p.netFlowTemplater()
		p.templateslock.Lock()
		p.templates[key] = templates
		p.templateslock.Unlock()
	}
	p.samplinglock.RLock()
	sampling, ok := p.sampling[key]
	p.samplinglock.RUnlock()
	if !ok {
		sampling = producer.CreateSamplingSystem()
		p.samplinglock.Lock()
		p.sampling[key] = sampling
		p.samplinglock.Unlock()
	}

	ts := uint64(pkt.Received.Unix())

	var packetV5 netflowlegacy.PacketNetFlowV5
	var packetNFv9 netflow.NFv9Packet
	var packetIPFIX netflow.IPFIXPacket

	// decode the version
	var version uint16
	if err := utils.BinaryDecoder(buf, &version); err != nil {
		return err
	}
	switch version {
	case 5:
		if err := netflowlegacy.DecodeMessage(buf, &packetV5); err != nil {
			return err
		}
	case 9:
		if err := netflow.DecodeMessageNetFlow(buf, templates, &packetNFv9); err != nil {
			return err
		}
	case 10:
		if err := netflow.DecodeMessageIPFIX(buf, templates, &packetIPFIX); err != nil {
			return err
		}
	default:
		return fmt.Errorf("Not a NetFlow packet")
	}

	var flowMessageSet []*flowmessage.FlowMessage
	var err error

	args := producer.ProcessArgs{
		Config:             p.configMapped,
		SamplingRateSystem: sampling,

		Src: pkt.Src,
		Dst: pkt.Dst,
	}

	if p.producer == nil {
		return nil
	}

	switch version {
	case 5:
		flowMessageSet, err = p.producer(packetV5, &args)
		if err != nil {
			return err
		}

	case 9:
		flowMessageSet, err = p.producer(&packetNFv9, &args)
		if err != nil {
			return err
		}

		for _, fmsg := range flowMessageSet {
			fmsg.TimeReceived = ts
			fmsg.SamplerAddress = samplerAddress
		}
	case 10:
		flowMessageSet, err = p.producer(&packetIPFIX, &args)
		if err != nil {
			return err
		}

		for _, fmsg := range flowMessageSet {
			fmsg.TimeReceived = ts
			fmsg.SamplerAddress = samplerAddress
		}
	}

	return p.formatSend(flowMessageSet)
}

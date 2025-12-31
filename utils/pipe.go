// Package utils provides flow pipeline and transport helpers.
package utils

import (
	"bytes"
	"fmt"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/decoders/utils"
	"github.com/netsampler/goflow2/v3/format"
	"github.com/netsampler/goflow2/v3/producer"
	"github.com/netsampler/goflow2/v3/transport"
	"github.com/netsampler/goflow2/v3/utils/templates"
)

// FlowPipe describes a flow decoder/formatter pipeline.
type FlowPipe interface {
	DecodeFlow(msg interface{}) error
	Close()
}

type flowpipe struct {
	format    format.FormatInterface
	transport transport.TransportInterface
	producer  producer.ProducerInterface

	netFlowRegistry templates.Registry
	templatesTTL    time.Duration
	sweepInterval   time.Duration
	extendOnAccess  bool
}

// PipeConfig wires formatter, transport, and producer dependencies.
type PipeConfig struct {
	Format    format.FormatInterface
	Transport transport.TransportInterface
	Producer  producer.ProducerInterface

	NetFlowRegistry templates.Registry
	TemplatesTTL    time.Duration
	SweepInterval   time.Duration
	ExtendOnAccess  bool
}

func (p *flowpipe) formatSend(flowMessageSet []producer.ProducerMessage) error {
	for _, msg := range flowMessageSet {
		// todo: pass normal
		if p.format != nil {
			key, data, err := p.format.Format(msg)
			if err != nil {
				return err
			}
			if p.transport != nil {
				if err = p.transport.Send(key, data); err != nil {
					return err
				}
			}
			// send to pool for reuse
		}
	}
	return nil

}

func (p *flowpipe) parseConfig(cfg *PipeConfig) {
	p.format = cfg.Format
	p.transport = cfg.Transport
	p.producer = cfg.Producer
	p.templatesTTL = cfg.TemplatesTTL
	p.sweepInterval = cfg.SweepInterval
	p.extendOnAccess = cfg.ExtendOnAccess
	if cfg.NetFlowRegistry != nil {
		p.netFlowRegistry = cfg.NetFlowRegistry
	} else {
		p.netFlowRegistry = templates.NewInMemoryRegistry(nil)
	}
	expiring, ok := p.netFlowRegistry.(*templates.ExpiringRegistry)
	if !ok {
		expiring = templates.NewExpiringRegistry(p.netFlowRegistry, p.templatesTTL)
		p.netFlowRegistry = expiring
	}
	expiring.SetExtendOnAccess(p.extendOnAccess)
	if p.sweepInterval > 0 {
		expiring.StartSweeper(p.sweepInterval)
	}

}

// SFlowPipe decodes sFlow packets and forwards them to a producer.
type SFlowPipe struct {
	flowpipe
}

// NetFlowPipe decodes NetFlow/IPFIX packets and forwards them to a producer.
type NetFlowPipe struct {
	flowpipe
}

// PipeMessageError wraps a decode/produce error with source message metadata.
type PipeMessageError struct {
	Message *Message
	Err     error
}

func (e *PipeMessageError) Error() string {
	return fmt.Sprintf("message from %s %s", e.Message.Src.String(), e.Err.Error())
}

func (e *PipeMessageError) Unwrap() error {
	return e.Err
}

// NewSFlowPipe creates a flow pipe configured for sFlow packets.
func NewSFlowPipe(cfg *PipeConfig) *SFlowPipe {
	p := &SFlowPipe{}
	p.parseConfig(cfg)
	return p
}

func (p *SFlowPipe) Close() {
}

// DecodeFlow decodes a sFlow payload and emits producer messages.
func (p *SFlowPipe) DecodeFlow(msg interface{}) error {
	pkt, ok := msg.(*Message)
	if !ok {
		return fmt.Errorf("flow is not *Message")
	}
	buf := bytes.NewBuffer(pkt.Payload)
	//key := pkt.Src.String()

	var packet sflow.Packet
	if err := sflow.DecodeMessageVersion(buf, &packet); err != nil {
		return &PipeMessageError{pkt, err}
	}

	args := producer.ProduceArgs{
		Src: pkt.Src,
		Dst: pkt.Dst,

		TimeReceived:   pkt.Received,
		SamplerAddress: pkt.Src.Addr(),
	}
	if p.producer == nil {
		return nil
	}
	flowMessageSet, err := p.producer.Produce(&packet, &args)
	defer p.producer.Commit(flowMessageSet)
	if err != nil {
		return &PipeMessageError{pkt, err}
	}
	return p.formatSend(flowMessageSet)
}

// NewNetFlowPipe creates a flow pipe configured for NetFlow/IPFIX packets.
func NewNetFlowPipe(cfg *PipeConfig) *NetFlowPipe {
	p := &NetFlowPipe{}
	p.parseConfig(cfg)
	return p
}

// DecodeFlow decodes a NetFlow/IPFIX payload and emits producer messages.
func (p *NetFlowPipe) DecodeFlow(msg interface{}) error {
	pkt, ok := msg.(*Message)
	if !ok {
		return fmt.Errorf("flow is not *Message")
	}
	buf := bytes.NewBuffer(pkt.Payload)

	key := pkt.Src.String()
	templates := p.netFlowRegistry.GetSystem(key)

	var packetV5 netflowlegacy.PacketNetFlowV5
	var packetNFv9 netflow.NFv9Packet
	var packetIPFIX netflow.IPFIXPacket

	// decode the version
	var version uint16
	if err := utils.BinaryDecoder(buf, &version); err != nil {
		return &PipeMessageError{pkt, err}
	}
	switch version {
	case 5:
		packetV5.Version = 5
		if err := netflowlegacy.DecodeMessage(buf, &packetV5); err != nil {
			return &PipeMessageError{pkt, err}
		}
	case 9:
		packetNFv9.Version = 9
		if err := netflow.DecodeMessageNetFlow(buf, templates, &packetNFv9); err != nil {
			return &PipeMessageError{pkt, err}
		}
	case 10:
		packetIPFIX.Version = 10
		if err := netflow.DecodeMessageIPFIX(buf, templates, &packetIPFIX); err != nil {
			return &PipeMessageError{pkt, err}
		}
	default:
		return &PipeMessageError{pkt, fmt.Errorf("not a NetFlow packet")}
	}

	var flowMessageSet []producer.ProducerMessage
	var err error

	args := producer.ProduceArgs{
		Src: pkt.Src,
		Dst: pkt.Dst,

		TimeReceived:   pkt.Received,
		SamplerAddress: pkt.Src.Addr(),
	}

	if p.producer == nil {
		return nil
	}

	switch version {
	case 5:
		flowMessageSet, err = p.producer.Produce(&packetV5, &args)
	case 9:
		flowMessageSet, err = p.producer.Produce(&packetNFv9, &args)
	case 10:
		flowMessageSet, err = p.producer.Produce(&packetIPFIX, &args)
	}
	defer p.producer.Commit(flowMessageSet)
	if err != nil {
		return &PipeMessageError{pkt, err}
	}

	return p.formatSend(flowMessageSet)
}

func (p *NetFlowPipe) Close() {
	p.netFlowRegistry.Close()
}

// GetTemplatesForAllSources returns a copy of templates for all known NetFlow sources.
func (p *NetFlowPipe) GetTemplatesForAllSources() map[string]map[uint64]interface{} {
	templates := p.netFlowRegistry.GetAll()
	ret := make(map[string]map[uint64]interface{}, len(templates))
	for key, systemTemplates := range templates {
		ret[key] = systemTemplates
	}
	return ret
}

// AutoFlowPipe dispatches to sFlow or NetFlow pipes based on payload.
type AutoFlowPipe struct {
	*SFlowPipe
	*NetFlowPipe
}

// NewFlowPipe creates a combined sFlow and NetFlow decoder.
func NewFlowPipe(cfg *PipeConfig) *AutoFlowPipe {
	p := &AutoFlowPipe{
		SFlowPipe:   NewSFlowPipe(cfg),
		NetFlowPipe: NewNetFlowPipe(cfg),
	}
	return p
}

func (p *AutoFlowPipe) Close() {
	p.SFlowPipe.Close()
	p.NetFlowPipe.Close()
}

// DecodeFlow detects the protocol and routes to the appropriate decoder.
func (p *AutoFlowPipe) DecodeFlow(msg interface{}) error {
	pkt, ok := msg.(*Message)
	if !ok {
		return fmt.Errorf("flow is not *Message")
	}
	buf := bytes.NewBuffer(pkt.Payload)

	var proto uint32
	if err := utils.BinaryDecoder(buf, &proto); err != nil {
		return &PipeMessageError{pkt, err}
	}

	protoNetFlow := (proto & 0xFFFF0000) >> 16
	if proto == 5 {
		return p.SFlowPipe.DecodeFlow(msg)
	} else if protoNetFlow == 5 || protoNetFlow == 9 || protoNetFlow == 10 {
		return p.NetFlowPipe.DecodeFlow(msg)
	}
	return fmt.Errorf("could not identify protocol %d", proto)
}

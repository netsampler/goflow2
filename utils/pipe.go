// Package utils provides flow pipeline and transport helpers.
package utils

import (
	"bytes"
	"fmt"

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
	Start()
	Close()
}

type flowpipe struct {
	format    format.FormatInterface
	transport transport.TransportInterface
	producer  producer.ProducerInterface

	netFlowRegistry templates.Registry
}

// PipeConfig wires formatter, transport, and producer dependencies.
type PipeConfig struct {
	Format    format.FormatInterface
	Transport transport.TransportInterface
	Producer  producer.ProducerInterface

	NetFlowRegistry templates.Registry
}

func (p *flowpipe) formatSend(flowMessageSet []producer.ProducerMessage) error {
	for _, msg := range flowMessageSet {
		// todo: pass normal
		if p.format != nil {
			key, data, err := p.format.Format(msg)
			if err != nil {
				return fmt.Errorf("format message: %w", err)
			}
			if p.transport != nil {
				if err = p.transport.Send(key, data); err != nil {
					return fmt.Errorf("send message: %w", err)
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
	if cfg.NetFlowRegistry != nil {
		p.netFlowRegistry = cfg.NetFlowRegistry
	} else {
		p.netFlowRegistry = templates.NewInMemoryRegistry(nil)
	}

}

func (p *flowpipe) Start() {
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
		return &PipeMessageError{pkt, fmt.Errorf("sflow decode: %w", err)}
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
		return &PipeMessageError{pkt, fmt.Errorf("sflow produce: %w", err)}
	}
	if err := p.formatSend(flowMessageSet); err != nil {
		return &PipeMessageError{pkt, fmt.Errorf("sflow format/send: %w", err)}
	}
	return nil
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
		return &PipeMessageError{pkt, fmt.Errorf("netflow version: %w", err)}
	}
	switch version {
	case 5:
		packetV5.Version = 5
		if err := netflowlegacy.DecodeMessage(buf, &packetV5); err != nil {
			return &PipeMessageError{pkt, fmt.Errorf("netflow v5 decode: %w", err)}
		}
	case 9:
		packetNFv9.Version = 9
		if err := netflow.DecodeMessageNetFlow(buf, templates, &packetNFv9); err != nil {
			return &PipeMessageError{pkt, fmt.Errorf("netflow v9 decode: %w", err)}
		}
	case 10:
		packetIPFIX.Version = 10
		if err := netflow.DecodeMessageIPFIX(buf, templates, &packetIPFIX); err != nil {
			return &PipeMessageError{pkt, fmt.Errorf("ipfix decode: %w", err)}
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
		return &PipeMessageError{pkt, fmt.Errorf("netflow produce: %w", err)}
	}
	if err := p.formatSend(flowMessageSet); err != nil {
		return &PipeMessageError{pkt, fmt.Errorf("netflow format/send: %w", err)}
	}
	return nil
}

func (p *NetFlowPipe) Close() {
}

// GetTemplatesForAllSources returns a copy of templates for all known NetFlow sources.
func (p *NetFlowPipe) GetTemplatesForAllSources() map[string]map[string]interface{} {
	templates := p.netFlowRegistry.GetAll()
	ret := make(map[string]map[string]interface{}, len(templates))
	for key, systemTemplates := range templates {
		formatted := make(map[string]interface{}, len(systemTemplates))
		for templateKey, template := range systemTemplates {
			formatted[formatTemplateKey(templateKey)] = template
		}
		ret[key] = formatted
	}
	return ret
}

func formatTemplateKey(key uint64) string {
	version := uint16(key >> 48)
	obsDomainId := uint32((key >> 16) & 0xFFFFFFFF)
	templateId := uint16(key & 0xFFFF)
	return fmt.Sprintf("%d/%d/%d", version, obsDomainId, templateId)
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

func (p *AutoFlowPipe) Start() {
	p.SFlowPipe.Start()
	p.NetFlowPipe.Start()
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
		return &PipeMessageError{pkt, fmt.Errorf("protocol detect: %w", err)}
	}

	protoNetFlow := (proto & 0xFFFF0000) >> 16
	if proto == 5 {
		return p.SFlowPipe.DecodeFlow(msg)
	} else if protoNetFlow == 5 || protoNetFlow == 9 || protoNetFlow == 10 {
		return p.NetFlowPipe.DecodeFlow(msg)
	}
	return fmt.Errorf("could not identify protocol %d", proto)
}

// Package rawproducer emits raw decoded packets for debugging or inspection.
package rawproducer

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
	"github.com/netsampler/goflow2/v2/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v2/decoders/sflow"
	"github.com/netsampler/goflow2/v2/producer"
)

// RawProducer emits messages without transforming decoded packets.
type RawProducer struct {
}

// RawMessage wraps a decoded packet with metadata.
type RawMessage struct {
	Message      interface{}    `json:"message"`
	Src          netip.AddrPort `json:"src"`
	TimeReceived time.Time      `json:"time_received"`
}

// MarshalJSON renders the raw message with a protocol type field.
func (m RawMessage) MarshalJSON() ([]byte, error) {
	typeStr := "unknown"
	switch m.Message.(type) {
	case *netflowlegacy.PacketNetFlowV5:
		typeStr = "netflowv5"
	case *netflow.NFv9Packet:
		typeStr = "netflowv9"
	case *netflow.IPFIXPacket:
		typeStr = "ipfix"
	case *sflow.Packet:
		typeStr = "sflow"
	}

	tmpStruct := struct {
		Type         string          `json:"type"`
		Message      interface{}     `json:"message"`
		Src          *netip.AddrPort `json:"src"`
		TimeReceived *time.Time      `json:"time_received"`
	}{
		Type:         typeStr,
		Message:      m.Message,
		Src:          &m.Src,
		TimeReceived: &m.TimeReceived,
	}
	return json.Marshal(tmpStruct)
}

// MarshalText renders a concise text summary of the raw message.
func (m RawMessage) MarshalText() ([]byte, error) {
	var msgContents []byte
	var err error
	if msg, ok := m.Message.(interface {
		MarshalText() ([]byte, error)
	}); ok {
		msgContents, err = msg.MarshalText()
	}
	return []byte(fmt.Sprintf("%s %s: %s", m.TimeReceived.String(), m.Src.String(), string(msgContents))), err
}

// Produce wraps the decoded packet into a RawMessage.
func (p *RawProducer) Produce(msg interface{}, args *producer.ProduceArgs) ([]producer.ProducerMessage, error) {
	// should return msg wrapped
	// []*interface{msg,}
	return []producer.ProducerMessage{RawMessage{msg, args.Src, args.TimeReceived}}, nil
}

// Commit is a no-op for RawProducer.
func (p *RawProducer) Commit(flowMessageSet []producer.ProducerMessage) {}

// Close is a no-op for RawProducer.
func (p *RawProducer) Close() {}

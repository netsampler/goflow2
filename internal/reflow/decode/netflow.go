package decode

import (
	"bytes"
	"fmt"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func (d *builtIn) decodeNetFlowV5(evt *event.Event, payload []byte) ([]*event.Event, error) {
	packet := &netflowlegacy.PacketNetFlowV5{}
	if err := netflowlegacy.DecodeMessageVersion(bytes.NewBuffer(payload), packet); err != nil {
		return nil, fmt.Errorf("decode netflow v5: %w", err)
	}

	out := make([]*event.Event, 0, len(packet.Records))
	for _, record := range packet.Records {
		item := cloneEvent(evt)
		fields := ensureFields(item, 16)
		fields["message_type"] = "flow"
		fields["flow_type"] = "netflowv5"
		fields["flow_version"] = packet.Version
		fields["src_addr"] = fmt.Sprint(record.SrcAddr)
		fields["dst_addr"] = fmt.Sprint(record.DstAddr)
		fields["src_port"] = uint32(record.SrcPort)
		fields["dst_port"] = uint32(record.DstPort)
		fields["proto"] = uint32(record.Proto)
		fields["bytes"] = int64(record.DOctets)
		fields["packets"] = int64(record.DPkts)
		fields["input_if"] = uint32(record.Input)
		fields["output_if"] = uint32(record.Output)
		fields["start_time_unix"] = flowTimeFromV5(packet.UnixSecs, packet.UnixNSecs, packet.SysUptime, record.First)
		fields["end_time_unix"] = flowTimeFromV5(packet.UnixSecs, packet.UnixNSecs, packet.SysUptime, record.Last)
		out = append(out, item)
	}
	return out, nil
}

func (d *builtIn) decodeNetFlowV9(evt *event.Event, payload []byte) ([]*event.Event, error) {
	packet := &netflow.NFv9Packet{}
	ctx := netflow.FlowContext{RouterKey: routerKey(evt)}
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payload), d.templates, ctx, packet, nil); err != nil {
		return nil, fmt.Errorf("decode netflow v9: %w", err)
	}

	var out []*event.Event
	for _, flowSet := range packet.FlowSets {
		dataSet, ok := flowSet.(netflow.DataFlowSet)
		if !ok {
			continue
		}
		for _, record := range dataSet.Records {
			item := cloneEvent(evt)
			fields := ensureFields(item, 16)
			fields["message_type"] = "flow"
			fields["flow_type"] = "netflowv9"
			fields["flow_version"] = packet.Version
			mapDataFields(fields, record.Values, packet.SystemUptime, packet.UnixSeconds)
			out = append(out, item)
		}
	}
	return out, nil
}

func (d *builtIn) decodeIPFIX(evt *event.Event, payload []byte) ([]*event.Event, error) {
	packet := &netflow.IPFIXPacket{}
	ctx := netflow.FlowContext{RouterKey: routerKey(evt)}
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payload), d.templates, ctx, nil, packet); err != nil {
		return nil, fmt.Errorf("decode ipfix: %w", err)
	}

	var out []*event.Event
	for _, flowSet := range packet.FlowSets {
		dataSet, ok := flowSet.(netflow.DataFlowSet)
		if !ok {
			continue
		}
		for _, record := range dataSet.Records {
			item := cloneEvent(evt)
			fields := ensureFields(item, 16)
			fields["message_type"] = "flow"
			fields["flow_type"] = "ipfix"
			fields["flow_version"] = packet.Version
			mapDataFields(fields, record.Values, 0, 0)
			out = append(out, item)
		}
	}
	return out, nil
}

func mapDataFields(fields map[string]any, values []netflow.DataField, sysUptime, unixSeconds uint32) {
	for _, field := range values {
		switch field.Type {
		case 4:
			fields["proto"] = decodeUint32(field.Value)
		case 7:
			fields["src_port"] = decodeUint32(field.Value)
		case 11:
			fields["dst_port"] = decodeUint32(field.Value)
		case 8, 27:
			fields["src_addr"] = decodeIPString(field.Value)
		case 12, 28:
			fields["dst_addr"] = decodeIPString(field.Value)
		case 1:
			fields["bytes"] = int64(decodeUint64(field.Value))
		case 2:
			fields["packets"] = int64(decodeUint64(field.Value))
		case 10:
			fields["input_if"] = decodeUint32(field.Value)
		case 14:
			fields["output_if"] = decodeUint32(field.Value)
		case 34:
			fields["sampling_rate"] = decodeUint32(field.Value)
		case netflow.NFV9_FIELD_FIRST_SWITCHED:
			fields["start_time_unix"] = flowTimeFromV9(sysUptime, unixSeconds, decodeUint32(field.Value))
		case netflow.NFV9_FIELD_LAST_SWITCHED:
			fields["end_time_unix"] = flowTimeFromV9(sysUptime, unixSeconds, decodeUint32(field.Value))
		case netflow.IPFIX_FIELD_flowStartMilliseconds:
			fields["start_time_unix"] = int64(decodeUint64(field.Value))
		case netflow.IPFIX_FIELD_flowEndMilliseconds:
			fields["end_time_unix"] = int64(decodeUint64(field.Value))
		}
	}
}

package decode

import (
	"bytes"
	"fmt"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
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
		fields["flow_type"] = "netflowv5"
		fields["flow_version"] = packet.Version
		fields["src_addr"] = fmt.Sprint(record.SrcAddr)
		fields["dst_addr"] = fmt.Sprint(record.DstAddr)
		fields["src_port"] = uint32(record.SrcPort)
		fields["dst_port"] = uint32(record.DstPort)
		fields["proto"] = uint32(record.Proto)
		fields["proto_name"] = ipProtocolName(uint32(record.Proto))
		fields["tcp_flags"] = uint32(record.TCPFlags)
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

// decodeNetFlowV9 expands a packet into template, options, and data events and
// keeps a sampling-rate cache keyed by exporter identity.
func (d *builtIn) decodeNetFlowV9(evt *event.Event, payload []byte) ([]*event.Event, error) {
	packet := &netflow.NFv9Packet{}
	ctx := netflow.FlowContext{RouterKey: routerKey(evt)}
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payload), d.templates, ctx, packet, nil); err != nil {
		return nil, fmt.Errorf("decode netflow v9: %w", err)
	}

	var out []*event.Event
	templatesFlowSet, optionsTemplatesFlowSet, optionsDataFlowSet := splitNetFlowV9Sets(packet)
	samplingRate, found, err := searchNetFlowOptionDataSets(optionsDataFlowSet)
	if err != nil {
		return nil, fmt.Errorf("netflow v9 options data sets: %w", err)
	}
	if d.sampling != nil {
		if found {
			_ = d.sampling.Set(ctx, packet.Version, packet.SourceId, samplingRate)
		} else if stored, ok, _ := d.sampling.Get(ctx, packet.Version, packet.SourceId); ok {
			samplingRate = stored
		}
	}
	out = append(out, d.templateEventsFromV9(evt, packet, templatesFlowSet, optionsTemplatesFlowSet)...)
	out = append(out, d.optionsEventsFromV9(evt, packet, optionsDataFlowSet)...)
	for _, flowSet := range packet.FlowSets {
		dataSet, ok := flowSet.(netflow.DataFlowSet)
		if !ok {
			continue
		}
		for _, record := range dataSet.Records {
			item := cloneEvent(evt)
			fields := ensureFields(item, 16)
			fields["flow_type"] = "netflowv9"
			fields["flow_version"] = packet.Version
			d.mapDataFields(fields, record.Values, packet.SystemUptime, packet.UnixSeconds, true)
			if fieldUint32(fields, "sampling_rate") == 0 && samplingRate != 0 {
				fields["sampling_rate"] = samplingRate
			}
			out = append(out, item)
		}
	}
	return out, nil
}

// decodeIPFIX mirrors the NetFlow v9 path but uses the IPFIX observation-domain
// identity and template semantics.
func (d *builtIn) decodeIPFIX(evt *event.Event, payload []byte) ([]*event.Event, error) {
	packet := &netflow.IPFIXPacket{}
	ctx := netflow.FlowContext{RouterKey: routerKey(evt)}
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payload), d.templates, ctx, nil, packet); err != nil {
		return nil, fmt.Errorf("decode ipfix: %w", err)
	}

	var out []*event.Event
	templatesFlowSet, optionsTemplatesFlowSet, optionsDataFlowSet := splitIPFIXSets(packet)
	samplingRate, found, err := searchNetFlowOptionDataSets(optionsDataFlowSet)
	if err != nil {
		return nil, fmt.Errorf("ipfix options data sets: %w", err)
	}
	if d.sampling != nil {
		if found {
			_ = d.sampling.Set(ctx, packet.Version, packet.ObservationDomainId, samplingRate)
		} else if stored, ok, _ := d.sampling.Get(ctx, packet.Version, packet.ObservationDomainId); ok {
			samplingRate = stored
		}
	}
	out = append(out, d.templateEventsFromIPFIX(evt, packet, templatesFlowSet, optionsTemplatesFlowSet)...)
	out = append(out, d.optionsEventsFromIPFIX(evt, packet, optionsDataFlowSet)...)
	for _, flowSet := range packet.FlowSets {
		dataSet, ok := flowSet.(netflow.DataFlowSet)
		if !ok {
			continue
		}
		for _, record := range dataSet.Records {
			item := cloneEvent(evt)
			fields := ensureFields(item, 16)
			fields["flow_type"] = "ipfix"
			fields["flow_version"] = packet.Version
			d.mapDataFields(fields, record.Values, 0, 0, false)
			if fieldUint32(fields, "sampling_rate") == 0 && samplingRate != 0 {
				fields["sampling_rate"] = samplingRate
			}
			out = append(out, item)
		}
	}
	return out, nil
}

// splitNetFlowV9Sets separates template-bearing sets from data sets so they can
// produce different control and data events downstream.
func splitNetFlowV9Sets(packet *netflow.NFv9Packet) ([]netflow.TemplateFlowSet, []netflow.NFv9OptionsTemplateFlowSet, []netflow.OptionsDataFlowSet) {
	var templatesFlowSet []netflow.TemplateFlowSet
	var optionsTemplatesFlowSet []netflow.NFv9OptionsTemplateFlowSet
	var optionsDataFlowSet []netflow.OptionsDataFlowSet
	for _, flowSet := range packet.FlowSets {
		switch tFlowSet := flowSet.(type) {
		case netflow.TemplateFlowSet:
			templatesFlowSet = append(templatesFlowSet, tFlowSet)
		case netflow.NFv9OptionsTemplateFlowSet:
			optionsTemplatesFlowSet = append(optionsTemplatesFlowSet, tFlowSet)
		case netflow.OptionsDataFlowSet:
			optionsDataFlowSet = append(optionsDataFlowSet, tFlowSet)
		}
	}
	return templatesFlowSet, optionsTemplatesFlowSet, optionsDataFlowSet
}

// splitIPFIXSets is the IPFIX equivalent of splitNetFlowV9Sets.
func splitIPFIXSets(packet *netflow.IPFIXPacket) ([]netflow.TemplateFlowSet, []netflow.IPFIXOptionsTemplateFlowSet, []netflow.OptionsDataFlowSet) {
	var templatesFlowSet []netflow.TemplateFlowSet
	var optionsTemplatesFlowSet []netflow.IPFIXOptionsTemplateFlowSet
	var optionsDataFlowSet []netflow.OptionsDataFlowSet
	for _, flowSet := range packet.FlowSets {
		switch tFlowSet := flowSet.(type) {
		case netflow.TemplateFlowSet:
			templatesFlowSet = append(templatesFlowSet, tFlowSet)
		case netflow.IPFIXOptionsTemplateFlowSet:
			optionsTemplatesFlowSet = append(optionsTemplatesFlowSet, tFlowSet)
		case netflow.OptionsDataFlowSet:
			optionsDataFlowSet = append(optionsDataFlowSet, tFlowSet)
		}
	}
	return templatesFlowSet, optionsTemplatesFlowSet, optionsDataFlowSet
}

// searchNetFlowOptionDataSets scans common sampling-rate field IDs across
// NetFlow/IPFIX options data records.
func searchNetFlowOptionDataSets(dataFlowSet []netflow.OptionsDataFlowSet) (uint32, bool, error) {
	var samplingRate uint32
	for _, dataFlowSetItem := range dataFlowSet {
		for _, record := range dataFlowSetItem.Records {
			if found, err := netFlowPopulate(record.OptionsValues, 305, &samplingRate); err != nil || found {
				return samplingRate, found, err
			}
			if found, err := netFlowPopulate(record.OptionsValues, 50, &samplingRate); err != nil || found {
				return samplingRate, found, err
			}
			if found, err := netFlowPopulate(record.OptionsValues, 34, &samplingRate); err != nil || found {
				return samplingRate, found, err
			}
		}
	}
	return samplingRate, false, nil
}

// netFlowPopulate copies one matching options field into the provided output.
func netFlowPopulate(dataFields []netflow.DataField, typeID uint16, out *uint32) (bool, error) {
	for _, dataField := range dataFields {
		if dataField.Type != typeID {
			continue
		}
		*out = decodeUint32(dataField.Value)
		return true, nil
	}
	return false, nil
}

// templateEventsFromV9 turns v9 templates into explicit runtime events so
// encoders can forward or translate template state.
func (d *builtIn) templateEventsFromV9(base *event.Event, packet *netflow.NFv9Packet, templatesSets []netflow.TemplateFlowSet, optionsSets []netflow.NFv9OptionsTemplateFlowSet) []*event.Event {
	var out []*event.Event
	for _, set := range templatesSets {
		for _, record := range set.Records {
			item := cloneEvent(base)
			fields := ensureFields(item, 8)
			fields["flow_type"] = "netflowv9_template"
			fields["flow_version"] = packet.Version
			fields["record_kind"] = "template"
			fields["template_id"] = uint32(record.TemplateId)
			fields["field_count"] = uint32(record.FieldCount)
			item.Payload = record
			out = append(out, item)
		}
	}
	for _, set := range optionsSets {
		for _, record := range set.Records {
			item := cloneEvent(base)
			fields := ensureFields(item, 8)
			fields["flow_type"] = "netflowv9_options_template"
			fields["flow_version"] = packet.Version
			fields["record_kind"] = "options_template"
			fields["tflow_record_type"] = "options"
			fields["template_id"] = uint32(record.TemplateId)
			fields["scope_field_count"] = uint32(len(record.Scopes))
			fields["option_field_count"] = uint32(len(record.Options))
			item.Payload = record
			out = append(out, item)
		}
	}
	return out
}

// templateEventsFromIPFIX turns IPFIX templates into explicit runtime events.
func (d *builtIn) templateEventsFromIPFIX(base *event.Event, packet *netflow.IPFIXPacket, templatesSets []netflow.TemplateFlowSet, optionsSets []netflow.IPFIXOptionsTemplateFlowSet) []*event.Event {
	var out []*event.Event
	for _, set := range templatesSets {
		for _, record := range set.Records {
			item := cloneEvent(base)
			fields := ensureFields(item, 8)
			fields["flow_type"] = "ipfix_template"
			fields["flow_version"] = packet.Version
			fields["record_kind"] = "template"
			fields["template_id"] = uint32(record.TemplateId)
			fields["field_count"] = uint32(record.FieldCount)
			item.Payload = record
			out = append(out, item)
		}
	}
	for _, set := range optionsSets {
		for _, record := range set.Records {
			item := cloneEvent(base)
			fields := ensureFields(item, 8)
			fields["flow_type"] = "ipfix_options_template"
			fields["flow_version"] = packet.Version
			fields["record_kind"] = "options_template"
			fields["tflow_record_type"] = "options"
			fields["template_id"] = uint32(record.TemplateId)
			fields["scope_field_count"] = uint32(record.ScopeFieldCount)
			fields["option_field_count"] = uint32(int(record.FieldCount) - int(record.ScopeFieldCount))
			item.Payload = record
			out = append(out, item)
		}
	}
	return out
}

// optionsEventsFromV9 wraps NetFlow v9 options records in runtime events.
func (d *builtIn) optionsEventsFromV9(base *event.Event, packet *netflow.NFv9Packet, optionsSets []netflow.OptionsDataFlowSet) []*event.Event {
	return d.optionsEvents(base, "netflowv9_options_data", packet.Version, optionsSets, true)
}

// optionsEventsFromIPFIX wraps IPFIX options records in runtime events.
func (d *builtIn) optionsEventsFromIPFIX(base *event.Event, packet *netflow.IPFIXPacket, optionsSets []netflow.OptionsDataFlowSet) []*event.Event {
	return d.optionsEvents(base, "ipfix_options_data", packet.Version, optionsSets, false)
}

// optionsEvents shares the event-building logic for NetFlow/IPFIX options data.
func (d *builtIn) optionsEvents(base *event.Event, flowType string, version uint16, optionsSets []netflow.OptionsDataFlowSet, netflowV9 bool) []*event.Event {
	var out []*event.Event
	for _, set := range optionsSets {
		for _, record := range set.Records {
			item := cloneEvent(base)
			fields := ensureFields(item, 8)
			fields["flow_type"] = flowType
			fields["flow_version"] = version
			fields["record_kind"] = "options_data"
			fields["tflow_record_type"] = "options"
			fields["template_id"] = uint32(set.Id)
			scopeKeys := d.mapOptionsFields(fields, record.ScopesValues, netflowV9, true, "scope")
			if len(scopeKeys) > 0 {
				fields["tflow.scope"] = scopeKeys
			}
			for _, dataField := range record.OptionsValues {
				if catalogField, ok := d.catalog.lookupOptions(dataField, netflowV9, false); ok {
					applyCatalogDataField(fields, dataField, catalogField, 0, 0, netflowV9)
					continue
				}
				switch dataField.Type {
				case 34, 50, 305:
					fields["sampling_rate"] = decodeUint32(dataField.Value)
				default:
					fields[fmt.Sprintf("option_%d", dataField.Type)] = decodeOptionFallback(dataField.Value)
				}
			}
			item.Payload = record
			out = append(out, item)
		}
	}
	return out
}

func (d *builtIn) mapOptionsFields(fields map[string]any, values []netflow.DataField, netflowV9 bool, scope bool, prefix string) []string {
	keys := make([]string, 0, len(values))
	for _, dataField := range values {
		if catalogField, ok := d.catalog.lookupOptions(dataField, netflowV9, scope); ok {
			applyCatalogDataField(fields, dataField, catalogField, 0, 0, netflowV9)
			keys = append(keys, catalogField.key)
			continue
		}
		key := fmt.Sprintf("%s_%d", prefix, dataField.Type)
		fields[key] = decodeOptionFallback(dataField.Value)
		keys = append(keys, key)
	}
	return keys
}

func decodeOptionFallback(val any) any {
	switch v := val.(type) {
	case []byte:
		return cloneRawBytes(v)
	default:
		return v
	}
}

// mapDataFields keeps the first cut of canonical field mapping close to the
// decode boundary so later stages can remain protocol-agnostic.
func (d *builtIn) mapDataFields(fields map[string]any, values []netflow.DataField, sysUptime, unixSeconds uint32, netflowV9 bool) []string {
	keys := make([]string, 0, len(values))
	for _, field := range values {
		if d != nil && !d.catalog.empty() {
			if catalogField, ok := d.catalog.lookup(field, netflowV9); ok {
				applyCatalogDataField(fields, field, catalogField, sysUptime, unixSeconds, netflowV9)
				keys = append(keys, catalogField.key)
			}
			continue
		}
		switch field.Type {
		case 4:
			fields["proto"] = decodeUint32(field.Value)
			fields["proto_name"] = ipProtocolName(decodeUint32(field.Value))
			keys = append(keys, "proto")
		case netflow.NFV9_FIELD_TCP_FLAGS:
			fields["tcp_flags"] = decodeUint32(field.Value)
			keys = append(keys, "tcp_flags")
		case 7:
			fields["src_port"] = decodeUint32(field.Value)
			keys = append(keys, "src_port")
		case 11:
			fields["dst_port"] = decodeUint32(field.Value)
			keys = append(keys, "dst_port")
		case 8, 27:
			fields["src_addr"] = decodeIPString(field.Value)
			keys = append(keys, "src_addr")
		case 12, 28:
			fields["dst_addr"] = decodeIPString(field.Value)
			keys = append(keys, "dst_addr")
		case 1:
			fields["bytes"] = int64(decodeUint64(field.Value))
			keys = append(keys, "bytes")
		case 2:
			fields["packets"] = int64(decodeUint64(field.Value))
			keys = append(keys, "packets")
		case 10:
			fields["input_if"] = decodeUint32(field.Value)
			keys = append(keys, "input_if")
		case 14:
			fields["output_if"] = decodeUint32(field.Value)
			keys = append(keys, "output_if")
		case 34:
			fields["sampling_rate"] = decodeUint32(field.Value)
			keys = append(keys, "sampling_rate")
		case netflow.NFV9_FIELD_FIRST_SWITCHED:
			fields["start_time_unix"] = flowTimeFromV9(sysUptime, unixSeconds, decodeUint32(field.Value))
			keys = append(keys, "start_time_unix")
		case netflow.NFV9_FIELD_LAST_SWITCHED:
			fields["end_time_unix"] = flowTimeFromV9(sysUptime, unixSeconds, decodeUint32(field.Value))
			keys = append(keys, "end_time_unix")
		case netflow.IPFIX_FIELD_flowStartMilliseconds:
			fields["start_time_unix"] = int64(decodeUint64(field.Value))
			keys = append(keys, "start_time_unix")
		case netflow.IPFIX_FIELD_flowEndMilliseconds:
			fields["end_time_unix"] = int64(decodeUint64(field.Value))
			keys = append(keys, "end_time_unix")
		}
	}
	return keys
}

package netflow

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/netsampler/goflow2/v3/decoders/utils"
)

const (
	netflowV9HeaderLen = 20
	ipfixHeaderLen     = 16
	flowSetHeaderLen   = 4
)

func (p *NFv9Packet) MarshalBinary() ([]byte, error) {
	return EncodeMessage(p)
}

func (p *IPFIXPacket) MarshalBinary() ([]byte, error) {
	return EncodeMessage(p)
}

func EncodeMessage(packet interface{}) ([]byte, error) {
	switch p := packet.(type) {
	case *NFv9Packet:
		return encodeNFv9Packet(p)
	case NFv9Packet:
		return encodeNFv9Packet(&p)
	case *IPFIXPacket:
		return encodeIPFIXPacket(p)
	case IPFIXPacket:
		return encodeIPFIXPacket(&p)
	default:
		return nil, fmt.Errorf("netflow: unsupported packet type %T", packet)
	}
}

func encodeNFv9Packet(packet *NFv9Packet) ([]byte, error) {
	if packet == nil {
		return nil, errors.New("netflow: nil packet")
	}

	version := packet.Version
	if version == 0 {
		version = 9
	}
	if version != 9 {
		return nil, fmt.Errorf("netflow: unsupported version %d", version)
	}

	flowSetPayload, err := encodeFlowSets(version, packet.FlowSets)
	if err != nil {
		return nil, err
	}

	count := packet.Count
	if count == 0 {
		count = uint16(len(packet.FlowSets))
	}

	buf := bytes.NewBuffer(make([]byte, 0, netflowV9HeaderLen+len(flowSetPayload)))
	if err := utils.WriteU16(buf, version); err != nil {
		return nil, err
	}
	if err := utils.WriteU16(buf, count); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.SystemUptime); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.UnixSeconds); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.SequenceNumber); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.SourceId); err != nil {
		return nil, err
	}
	if _, err := buf.Write(flowSetPayload); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeIPFIXPacket(packet *IPFIXPacket) ([]byte, error) {
	if packet == nil {
		return nil, errors.New("netflow: nil packet")
	}

	version := packet.Version
	if version == 0 {
		version = 10
	}
	if version != 10 {
		return nil, fmt.Errorf("netflow: unsupported version %d", version)
	}

	flowSetPayload, err := encodeFlowSets(version, packet.FlowSets)
	if err != nil {
		return nil, err
	}

	length := packet.Length
	if length == 0 {
		length = uint16(ipfixHeaderLen + len(flowSetPayload))
	}

	buf := bytes.NewBuffer(make([]byte, 0, int(length)))
	if err := utils.WriteU16(buf, version); err != nil {
		return nil, err
	}
	if err := utils.WriteU16(buf, length); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.ExportTime); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.SequenceNumber); err != nil {
		return nil, err
	}
	if err := utils.WriteU32(buf, packet.ObservationDomainId); err != nil {
		return nil, err
	}
	if _, err := buf.Write(flowSetPayload); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeFlowSets(version uint16, flowSets []interface{}) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	templates := make(map[uint16]interface{})
	for _, flowSet := range flowSets {
		payload, id, err := encodeFlowSet(version, flowSet, templates)
		if err != nil {
			return nil, err
		}
		if err := utils.WriteU16(buf, id); err != nil {
			return nil, err
		}
		if err := utils.WriteU16(buf, uint16(flowSetHeaderLen+len(payload))); err != nil {
			return nil, err
		}
		if _, err := buf.Write(payload); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func encodeFlowSet(version uint16, flowSet interface{}, templates map[uint16]interface{}) ([]byte, uint16, error) {
	switch fs := flowSet.(type) {
	case TemplateFlowSet:
		payload, id, err := encodeTemplateFlowSet(version, &fs)
		if err == nil {
			for _, record := range fs.Records {
				templates[record.TemplateId] = record
			}
		}
		return payload, id, err
	case *TemplateFlowSet:
		payload, id, err := encodeTemplateFlowSet(version, fs)
		if err == nil {
			for _, record := range fs.Records {
				templates[record.TemplateId] = record
			}
		}
		return payload, id, err
	case NFv9OptionsTemplateFlowSet:
		if version != 9 {
			return nil, 0, fmt.Errorf("netflow: invalid flow set %T for version %d", flowSet, version)
		}
		payload, id, err := encodeNFv9OptionsTemplateFlowSet(&fs)
		if err == nil {
			for _, record := range fs.Records {
				templates[record.TemplateId] = record
			}
		}
		return payload, id, err
	case *NFv9OptionsTemplateFlowSet:
		if version != 9 {
			return nil, 0, fmt.Errorf("netflow: invalid flow set %T for version %d", flowSet, version)
		}
		payload, id, err := encodeNFv9OptionsTemplateFlowSet(fs)
		if err == nil {
			for _, record := range fs.Records {
				templates[record.TemplateId] = record
			}
		}
		return payload, id, err
	case IPFIXOptionsTemplateFlowSet:
		if version != 10 {
			return nil, 0, fmt.Errorf("netflow: invalid flow set %T for version %d", flowSet, version)
		}
		payload, id, err := encodeIPFIXOptionsTemplateFlowSet(&fs)
		if err == nil {
			for _, record := range fs.Records {
				templates[record.TemplateId] = record
			}
		}
		return payload, id, err
	case *IPFIXOptionsTemplateFlowSet:
		if version != 10 {
			return nil, 0, fmt.Errorf("netflow: invalid flow set %T for version %d", flowSet, version)
		}
		payload, id, err := encodeIPFIXOptionsTemplateFlowSet(fs)
		if err == nil {
			for _, record := range fs.Records {
				templates[record.TemplateId] = record
			}
		}
		return payload, id, err
	case DataFlowSet:
		return encodeDataFlowSet(&fs, templates[fs.Id])
	case *DataFlowSet:
		return encodeDataFlowSet(fs, templates[fs.Id])
	case OptionsDataFlowSet:
		return encodeOptionsDataFlowSet(&fs, templates[fs.Id])
	case *OptionsDataFlowSet:
		return encodeOptionsDataFlowSet(fs, templates[fs.Id])
	case RawFlowSet:
		return encodeRawFlowSet(&fs)
	case *RawFlowSet:
		return encodeRawFlowSet(fs)
	default:
		return nil, 0, fmt.Errorf("netflow: unsupported flow set type %T", flowSet)
	}
}

func encodeTemplateFlowSet(version uint16, flowSet *TemplateFlowSet) ([]byte, uint16, error) {
	if flowSet == nil {
		return nil, 0, errors.New("netflow: nil template flow set")
	}

	id := flowSet.Id
	if id == 0 {
		switch version {
		case 9:
			id = 0
		case 10:
			id = 2
		}
	}
	if (version == 9 && id != 0) || (version == 10 && id != 2) {
		return nil, 0, fmt.Errorf("netflow: invalid template flow set id %d for version %d", id, version)
	}

	buf := bytes.NewBuffer(nil)
	for _, record := range flowSet.Records {
		fieldCount := record.FieldCount
		if fieldCount == 0 {
			fieldCount = uint16(len(record.Fields))
		}
		if int(fieldCount) != len(record.Fields) {
			return nil, 0, fmt.Errorf("netflow: template field-count mismatch header:%d fields:%d", fieldCount, len(record.Fields))
		}

		if err := utils.WriteU16(buf, record.TemplateId); err != nil {
			return nil, 0, err
		}
		if err := utils.WriteU16(buf, fieldCount); err != nil {
			return nil, 0, err
		}
		for _, field := range record.Fields {
			if err := encodeField(buf, field, version == 10); err != nil {
				return nil, 0, err
			}
		}
	}
	return padFlowSetPayload(buf.Bytes()), id, nil
}

func encodeNFv9OptionsTemplateFlowSet(flowSet *NFv9OptionsTemplateFlowSet) ([]byte, uint16, error) {
	if flowSet == nil {
		return nil, 0, errors.New("netflow: nil options template flow set")
	}

	id := flowSet.Id
	if id == 0 {
		id = 1
	}
	if id != 1 {
		return nil, 0, fmt.Errorf("netflow: invalid NetFlow v9 options template flow set id %d", id)
	}

	buf := bytes.NewBuffer(nil)
	for _, record := range flowSet.Records {
		scopeLength := record.ScopeLength
		if scopeLength == 0 {
			scopeLength = uint16(len(record.Scopes) * 4)
		}
		optionLength := record.OptionLength
		if optionLength == 0 {
			optionLength = uint16(len(record.Options) * 4)
		}
		if int(scopeLength/4) != len(record.Scopes) {
			return nil, 0, fmt.Errorf("netflow: scope-length mismatch header:%d scopes:%d", scopeLength, len(record.Scopes))
		}
		if int(optionLength/4) != len(record.Options) {
			return nil, 0, fmt.Errorf("netflow: option-length mismatch header:%d options:%d", optionLength, len(record.Options))
		}

		if err := utils.WriteU16(buf, record.TemplateId); err != nil {
			return nil, 0, err
		}
		if err := utils.WriteU16(buf, scopeLength); err != nil {
			return nil, 0, err
		}
		if err := utils.WriteU16(buf, optionLength); err != nil {
			return nil, 0, err
		}
		for _, field := range record.Scopes {
			if err := encodeField(buf, field, false); err != nil {
				return nil, 0, err
			}
		}
		for _, field := range record.Options {
			if err := encodeField(buf, field, false); err != nil {
				return nil, 0, err
			}
		}
	}
	return padFlowSetPayload(buf.Bytes()), id, nil
}

func encodeIPFIXOptionsTemplateFlowSet(flowSet *IPFIXOptionsTemplateFlowSet) ([]byte, uint16, error) {
	if flowSet == nil {
		return nil, 0, errors.New("netflow: nil IPFIX options template flow set")
	}

	id := flowSet.Id
	if id == 0 {
		id = 3
	}
	if id != 3 {
		return nil, 0, fmt.Errorf("netflow: invalid IPFIX options template flow set id %d", id)
	}

	buf := bytes.NewBuffer(nil)
	for _, record := range flowSet.Records {
		fieldCount := record.FieldCount
		if fieldCount == 0 {
			fieldCount = uint16(len(record.Scopes) + len(record.Options))
		}
		scopeFieldCount := record.ScopeFieldCount
		if scopeFieldCount == 0 && len(record.Scopes) > 0 {
			scopeFieldCount = uint16(len(record.Scopes))
		}
		if int(fieldCount) != len(record.Scopes)+len(record.Options) {
			return nil, 0, fmt.Errorf("netflow: field-count mismatch header:%d fields:%d", fieldCount, len(record.Scopes)+len(record.Options))
		}
		if int(scopeFieldCount) != len(record.Scopes) {
			return nil, 0, fmt.Errorf("netflow: scope-field-count mismatch header:%d scopes:%d", scopeFieldCount, len(record.Scopes))
		}

		if err := utils.WriteU16(buf, record.TemplateId); err != nil {
			return nil, 0, err
		}
		if err := utils.WriteU16(buf, fieldCount); err != nil {
			return nil, 0, err
		}
		if err := utils.WriteU16(buf, scopeFieldCount); err != nil {
			return nil, 0, err
		}
		for _, field := range record.Scopes {
			if err := encodeField(buf, field, true); err != nil {
				return nil, 0, err
			}
		}
		for _, field := range record.Options {
			if err := encodeField(buf, field, true); err != nil {
				return nil, 0, err
			}
		}
	}
	return padFlowSetPayload(buf.Bytes()), id, nil
}

func encodeDataFlowSet(flowSet *DataFlowSet, template interface{}) ([]byte, uint16, error) {
	if flowSet == nil {
		return nil, 0, errors.New("netflow: nil data flow set")
	}
	if flowSet.Id < 256 {
		return nil, 0, fmt.Errorf("netflow: invalid data flow set id %d", flowSet.Id)
	}

	buf := bytes.NewBuffer(nil)
	for _, record := range flowSet.Records {
		fields, err := templateFields(template, len(record.Values))
		if err != nil {
			return nil, 0, err
		}
		for i, field := range record.Values {
			var tmpl *Field
			if fields != nil {
				tmpl = &fields[i]
			}
			if err := encodeDataFieldValue(buf, field, tmpl); err != nil {
				return nil, 0, err
			}
		}
	}
	return padFlowSetPayload(buf.Bytes()), flowSet.Id, nil
}

func encodeOptionsDataFlowSet(flowSet *OptionsDataFlowSet, template interface{}) ([]byte, uint16, error) {
	if flowSet == nil {
		return nil, 0, errors.New("netflow: nil options data flow set")
	}
	if flowSet.Id < 256 {
		return nil, 0, fmt.Errorf("netflow: invalid options data flow set id %d", flowSet.Id)
	}

	buf := bytes.NewBuffer(nil)
	for _, record := range flowSet.Records {
		scopeFields, optionFields, err := optionTemplateFields(template, len(record.ScopesValues), len(record.OptionsValues))
		if err != nil {
			return nil, 0, err
		}
		for i, field := range record.ScopesValues {
			var tmpl *Field
			if scopeFields != nil {
				tmpl = &scopeFields[i]
			}
			if err := encodeDataFieldValue(buf, field, tmpl); err != nil {
				return nil, 0, err
			}
		}
		for i, field := range record.OptionsValues {
			var tmpl *Field
			if optionFields != nil {
				tmpl = &optionFields[i]
			}
			if err := encodeDataFieldValue(buf, field, tmpl); err != nil {
				return nil, 0, err
			}
		}
	}
	return padFlowSetPayload(buf.Bytes()), flowSet.Id, nil
}

func encodeRawFlowSet(flowSet *RawFlowSet) ([]byte, uint16, error) {
	if flowSet == nil {
		return nil, 0, errors.New("netflow: nil raw flow set")
	}
	if flowSet.Id < 256 {
		return nil, 0, fmt.Errorf("netflow: invalid raw flow set id %d", flowSet.Id)
	}
	return padFlowSetPayload(flowSet.Records), flowSet.Id, nil
}

func encodeField(buf *bytes.Buffer, field Field, pen bool) error {
	typeID := field.Type
	if pen && field.PenProvided {
		typeID |= 0x8000
	}
	if err := utils.WriteU16(buf, typeID); err != nil {
		return err
	}
	if err := utils.WriteU16(buf, field.Length); err != nil {
		return err
	}
	if pen && field.PenProvided {
		if err := utils.WriteU32(buf, field.Pen); err != nil {
			return err
		}
	}
	return nil
}

func encodeDataFieldValue(buf *bytes.Buffer, field DataField, template *Field) error {
	value, err := asBytes(field.Value)
	if err != nil {
		return fmt.Errorf("netflow: encode field %d: %w", field.Type, err)
	}

	if template != nil && template.Length == 0xffff {
		if len(value) < 255 {
			if err := utils.WriteU8(buf, uint8(len(value))); err != nil {
				return err
			}
		} else {
			if err := utils.WriteU8(buf, 0xff); err != nil {
				return err
			}
			if err := utils.WriteU16(buf, uint16(len(value))); err != nil {
				return err
			}
		}
	} else if template != nil && template.Length != 0 && int(template.Length) != len(value) {
		return fmt.Errorf("length mismatch header:%d value:%d", template.Length, len(value))
	}

	_, err = buf.Write(value)
	return err
}

func templateFields(template interface{}, values int) ([]Field, error) {
	switch tmpl := template.(type) {
	case nil:
		return nil, nil
	case TemplateRecord:
		if len(tmpl.Fields) != values {
			return nil, fmt.Errorf("netflow: template field-count mismatch template:%d values:%d", len(tmpl.Fields), values)
		}
		return tmpl.Fields, nil
	default:
		return nil, fmt.Errorf("netflow: invalid data template type %T", template)
	}
}

func optionTemplateFields(template interface{}, scopes, options int) ([]Field, []Field, error) {
	switch tmpl := template.(type) {
	case nil:
		return nil, nil, nil
	case NFv9OptionsTemplateRecord:
		if len(tmpl.Scopes) != scopes {
			return nil, nil, fmt.Errorf("netflow: scope template mismatch template:%d values:%d", len(tmpl.Scopes), scopes)
		}
		if len(tmpl.Options) != options {
			return nil, nil, fmt.Errorf("netflow: option template mismatch template:%d values:%d", len(tmpl.Options), options)
		}
		return tmpl.Scopes, tmpl.Options, nil
	case IPFIXOptionsTemplateRecord:
		if len(tmpl.Scopes) != scopes {
			return nil, nil, fmt.Errorf("netflow: scope template mismatch template:%d values:%d", len(tmpl.Scopes), scopes)
		}
		if len(tmpl.Options) != options {
			return nil, nil, fmt.Errorf("netflow: option template mismatch template:%d values:%d", len(tmpl.Options), options)
		}
		return tmpl.Scopes, tmpl.Options, nil
	default:
		return nil, nil, fmt.Errorf("netflow: invalid options template type %T", template)
	}
}

func asBytes(v interface{}) ([]byte, error) {
	switch data := v.(type) {
	case []byte:
		return data, nil
	case string:
		return []byte(data), nil
	default:
		return nil, fmt.Errorf("unsupported value type %T", v)
	}
}

func padFlowSetPayload(payload []byte) []byte {
	padding := (4 - ((flowSetHeaderLen + len(payload)) % 4)) % 4
	if padding == 0 {
		return payload
	}
	ret := make([]byte, len(payload)+padding)
	copy(ret, payload)
	return ret
}

package netflow

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/netsampler/goflow2/v2/decoders/utils"
)

type DecoderError struct {
	Decoder string
	Err     error
}

func (e *DecoderError) Error() string {
	return fmt.Sprintf("%s %s", e.Decoder, e.Err.Error())
}

func (e *DecoderError) Unwrap() error {
	return e.Err
}

type FlowError struct {
	Version     uint16
	Type        string
	ObsDomainId uint32
	TemplateId  uint16
	Err         error
}

func (e *FlowError) Error() string {
	return fmt.Sprintf("[version:%d type:%s obsDomainId:%v: templateId:%d] %s", e.Version, e.Type, e.ObsDomainId, e.TemplateId, e.Err.Error())
}

func (e *FlowError) Unwrap() error {
	return e.Err
}

func DecodeNFv9OptionsTemplateSet(payload *bytes.Buffer) ([]NFv9OptionsTemplateRecord, error) {
	var records []NFv9OptionsTemplateRecord
	var err error
	for payload.Len() >= 4 {
		optsTemplateRecord := NFv9OptionsTemplateRecord{}
		err = utils.BinaryDecoder(payload,
			&optsTemplateRecord.TemplateId,
			&optsTemplateRecord.ScopeLength,
			&optsTemplateRecord.OptionLength,
		)
		if err != nil {
			return records, err
		}

		sizeScope := int(optsTemplateRecord.ScopeLength) / 4
		sizeOptions := int(optsTemplateRecord.OptionLength) / 4
		if sizeScope < 0 || sizeOptions < 0 {
			return records, fmt.Errorf("NFv9OptionsTemplateSet: negative length")
		}

		fields := make([]Field, sizeScope) // max 16383 entries, 65KB
		for i := 0; i < sizeScope; i++ {
			field := Field{}
			if err := DecodeField(payload, &field, false); err != nil {
				return records, fmt.Errorf("NFv9OptionsTemplateSet: scope:%d [%w]", i, err)
			}
			fields[i] = field
		}
		optsTemplateRecord.Scopes = fields

		fields = make([]Field, sizeOptions)
		for i := 0; i < sizeOptions; i++ {
			field := Field{}
			if err := DecodeField(payload, &field, false); err != nil {
				return records, fmt.Errorf("NFv9OptionsTemplateSet: option:%d [%w]", i, err)
			}
			fields[i] = field
		}
		optsTemplateRecord.Options = fields

		records = append(records, optsTemplateRecord)
	}

	return records, err
}

func DecodeField(payload *bytes.Buffer, field *Field, pen bool) error {
	if err := utils.BinaryDecoder(payload,
		&field.Type,
		&field.Length,
	); err != nil {
		return err
	}
	if pen && field.Type&0x8000 != 0 {
		field.PenProvided = true
		return utils.BinaryDecoder(payload,
			&field.Pen,
		)
	}
	return nil
}

func DecodeIPFIXOptionsTemplateSet(payload *bytes.Buffer) ([]IPFIXOptionsTemplateRecord, error) {
	var records []IPFIXOptionsTemplateRecord
	var err error
	for payload.Len() >= 4 {
		optsTemplateRecord := IPFIXOptionsTemplateRecord{}
		err = utils.BinaryDecoder(payload,
			&optsTemplateRecord.TemplateId,
			&optsTemplateRecord.FieldCount,
			&optsTemplateRecord.ScopeFieldCount)
		if err != nil {
			return records, fmt.Errorf("IPFIXOptionsTemplateSet: header [%w]", err)
		}

		fields := make([]Field, int(optsTemplateRecord.ScopeFieldCount)) // max 65532 which would be 589KB
		for i := 0; i < int(optsTemplateRecord.ScopeFieldCount); i++ {
			field := Field{}
			if err := DecodeField(payload, &field, true); err != nil {
				return records, fmt.Errorf("IPFIXOptionsTemplateSet: scope:%d [%w]", i, err)
			}
			fields[i] = field
		}
		optsTemplateRecord.Scopes = fields

		optionsSize := int(optsTemplateRecord.FieldCount) - int(optsTemplateRecord.ScopeFieldCount)
		if optionsSize < 0 {
			return records, fmt.Errorf("IPFIXOptionsTemplateSet: negative length")
		}
		fields = make([]Field, optionsSize)
		for i := 0; i < optionsSize; i++ {
			field := Field{}
			if err := DecodeField(payload, &field, true); err != nil {
				return records, fmt.Errorf("IPFIXOptionsTemplateSet: option:%d [%w]", i, err)
			}
			fields[i] = field
		}
		optsTemplateRecord.Options = fields

		records = append(records, optsTemplateRecord)
	}

	return records, nil
}

func DecodeTemplateSet(version uint16, payload *bytes.Buffer) ([]TemplateRecord, error) {
	var records []TemplateRecord
	var err error
	for payload.Len() >= 4 {
		templateRecord := TemplateRecord{}
		err = utils.BinaryDecoder(payload,
			&templateRecord.TemplateId,
			&templateRecord.FieldCount,
		)
		if err != nil {
			return records, fmt.Errorf("TemplateSet: reading header [%w]", err)
		}

		if int(templateRecord.FieldCount) < 0 {
			return records, fmt.Errorf("TemplateSet: zero count")
		}

		fields := make([]Field, int(templateRecord.FieldCount)) // max 65532 which would be 589KB
		for i := 0; i < int(templateRecord.FieldCount); i++ {
			field := Field{}
			if err := utils.BinaryDecoder(payload,
				&field.Type,
				&field.Length,
			); err != nil {
				return records, fmt.Errorf("TemplateSet: reading field [%w]", err)
			}
			if version == 10 && field.Type&0x8000 != 0 {
				field.PenProvided = true
				field.Type = field.Type ^ 0x8000
				if err := utils.BinaryDecoder(payload,
					&field.Pen,
				); err != nil {
					return records, fmt.Errorf("TemplateSet: reading enterprise field [%w]", err)
				}
			}
			fields[i] = field
		}
		templateRecord.Fields = fields
		records = append(records, templateRecord)
	}

	return records, nil
}

func GetTemplateSize(version uint16, template []Field) int {
	sum := 0
	for _, templateField := range template {
		if templateField.Length == 0xffff {
			continue
		}
		sum += int(templateField.Length)
	}
	return sum
}

func DecodeDataSetUsingFields(version uint16, payload *bytes.Buffer, listFields []Field) ([]DataField, error) {
	dataFields := make([]DataField, len(listFields))
	if payload.Len() >= GetTemplateSize(version, listFields) {

		for i, templateField := range listFields {

			finalLength := int(templateField.Length)
			if templateField.Length == 0xffff {
				var variableLen8 byte
				var variableLen16 uint16
				if err := utils.BinaryDecoder(payload,
					&variableLen8,
				); err != nil {
					return nil, err
				}
				if variableLen8 == 0xff {
					if err := utils.BinaryDecoder(payload,
						&variableLen16,
					); err != nil {
						return nil, err
					}
					finalLength = int(variableLen16)
				} else {
					finalLength = int(variableLen8)
				}
			}

			value := payload.Next(finalLength)
			nfvalue := DataField{
				Type:        templateField.Type,
				PenProvided: templateField.PenProvided,
				Pen:         templateField.Pen,
				Value:       value,
			}
			dataFields[i] = nfvalue
		}
	}
	return dataFields, nil
}

func DecodeOptionsDataSet(version uint16, payload *bytes.Buffer, listFieldsScopes, listFieldsOption []Field) ([]OptionsDataRecord, error) {
	var records []OptionsDataRecord

	listFieldsScopesSize := GetTemplateSize(version, listFieldsScopes)
	listFieldsOptionSize := GetTemplateSize(version, listFieldsOption)

	for payload.Len() >= listFieldsScopesSize+listFieldsOptionSize {
		scopeValues, err := DecodeDataSetUsingFields(version, payload, listFieldsScopes)
		if err != nil {
			return records, fmt.Errorf("OptionsDataSet: scope [%w]", err)
		}
		optionValues, err := DecodeDataSetUsingFields(version, payload, listFieldsOption)
		if err != nil {
			return records, fmt.Errorf("OptionsDataSet: options [%w]", err)
		}

		record := OptionsDataRecord{
			ScopesValues:  scopeValues,
			OptionsValues: optionValues,
		}

		records = append(records, record)
	}
	return records, nil
}

func DecodeDataSet(version uint16, payload *bytes.Buffer, listFields []Field) ([]DataRecord, error) {
	var records []DataRecord

	listFieldsSize := GetTemplateSize(version, listFields)
	for payload.Len() >= listFieldsSize {
		values, err := DecodeDataSetUsingFields(version, payload, listFields)
		if err != nil {
			return records, fmt.Errorf("DataSet: fields [%w]", err)
		}

		record := DataRecord{
			Values: values,
		}

		records = append(records, record)
	}
	return records, nil
}

func DecodeMessageCommon(payload *bytes.Buffer, templates NetFlowTemplateSystem, obsDomainId uint32, size, version uint16) (flowSets []interface{}, err error) {
	var read int
	startSize := payload.Len()
	for i := 0; ((i < int(size) && version == 9) || (uint16(read) < size && version == 10)) && payload.Len() > 0; i++ {
		if flowSet, lerr := DecodeMessageCommonFlowSet(payload, templates, obsDomainId, version); lerr != nil && !errors.Is(lerr, ErrorTemplateNotFound) {
			return flowSets, lerr
		} else {
			flowSets = append(flowSets, flowSet)
			if lerr != nil {
				err = errors.Join(err, lerr)
			}
		}
		read = startSize - payload.Len()
	}
	return flowSets, err
}

func DecodeMessageCommonFlowSet(payload *bytes.Buffer, templates NetFlowTemplateSystem, obsDomainId uint32, version uint16) (flowSet interface{}, err error) {
	fsheader := FlowSetHeader{}
	if err := utils.BinaryDecoder(payload,
		&fsheader.Id,
		&fsheader.Length,
	); err != nil {
		return flowSet, fmt.Errorf("header [%w]", err)
	}

	nextrelpos := int(fsheader.Length) - binary.Size(fsheader)
	if nextrelpos < 0 {
		return flowSet, fmt.Errorf("negative length")
	}

	if fsheader.Id == 0 && version == 9 {
		templateReader := bytes.NewBuffer(payload.Next(nextrelpos))
		records, err := DecodeTemplateSet(version, templateReader)
		if err != nil {
			return flowSet, &FlowError{version, "FlowSet", obsDomainId, fsheader.Id, err}
		}
		templatefs := TemplateFlowSet{
			FlowSetHeader: fsheader,
			Records:       records,
		}

		flowSet = templatefs

		if templates != nil {
			for _, record := range records {
				if err := templates.AddTemplate(version, obsDomainId, record.TemplateId, record); err != nil {
					return flowSet, &FlowError{version, "FlowSet", obsDomainId, fsheader.Id, err}
				}
			}
		}

	} else if fsheader.Id == 1 && version == 9 {
		templateReader := bytes.NewBuffer(payload.Next(nextrelpos))
		records, err := DecodeNFv9OptionsTemplateSet(templateReader)
		if err != nil {
			return flowSet, &FlowError{version, "NetFlow OptionsTemplateSet", obsDomainId, fsheader.Id, err}
		}
		optsTemplatefs := NFv9OptionsTemplateFlowSet{
			FlowSetHeader: fsheader,
			Records:       records,
		}
		flowSet = optsTemplatefs

		if templates != nil {
			for _, record := range records {
				if err := templates.AddTemplate(version, obsDomainId, record.TemplateId, record); err != nil {
					return flowSet, &FlowError{version, "OptionsTemplateSet", obsDomainId, fsheader.Id, err}
				}
			}
		}

	} else if fsheader.Id == 2 && version == 10 {
		templateReader := bytes.NewBuffer(payload.Next(nextrelpos))
		records, err := DecodeTemplateSet(version, templateReader)
		if err != nil {
			return flowSet, &FlowError{version, "IPFIX TemplateSet", obsDomainId, fsheader.Id, err}
		}
		templatefs := TemplateFlowSet{
			FlowSetHeader: fsheader,
			Records:       records,
		}
		flowSet = templatefs

		if templates != nil {
			for _, record := range records {
				if err := templates.AddTemplate(version, obsDomainId, record.TemplateId, record); err != nil {
					return flowSet, &FlowError{version, "IPFIX TemplateSet", obsDomainId, fsheader.Id, err}
				}
			}
		}

	} else if fsheader.Id == 3 && version == 10 {
		templateReader := bytes.NewBuffer(payload.Next(nextrelpos))
		records, err := DecodeIPFIXOptionsTemplateSet(templateReader)
		if err != nil {
			return flowSet, &FlowError{version, "IPFIX OptionsTemplateSet", obsDomainId, fsheader.Id, err}
		}
		optsTemplatefs := IPFIXOptionsTemplateFlowSet{
			FlowSetHeader: fsheader,
			Records:       records,
		}
		flowSet = optsTemplatefs

		if templates != nil {
			for _, record := range records {
				if err := templates.AddTemplate(version, obsDomainId, record.TemplateId, record); err != nil {
					return flowSet, &FlowError{version, "IPFIX OptionsTemplateSet", obsDomainId, fsheader.Id, err}
				}
			}
		}

	} else if fsheader.Id >= 256 {
		rawfs := RawFlowSet{
			FlowSetHeader: fsheader,
			Records:       payload.Next(nextrelpos),
		}
		flowSet = rawfs
		dataReader := bytes.NewBuffer(rawfs.Records)

		if templates == nil {
			return flowSet, &FlowError{version, "Templates", obsDomainId, fsheader.Id, fmt.Errorf("No templates")}
		}

		template, err := templates.GetTemplate(version, obsDomainId, fsheader.Id)
		if err != nil {
			return flowSet, &FlowError{version, "Decode", obsDomainId, fsheader.Id, err}
		}

		switch templatec := template.(type) {
		case TemplateRecord:
			records, err := DecodeDataSet(version, dataReader, templatec.Fields)
			if err != nil {
				return flowSet, &FlowError{version, "DataSet", obsDomainId, fsheader.Id, err}
			}
			datafs := DataFlowSet{
				FlowSetHeader: fsheader,
				Records:       records,
			}
			flowSet = datafs
		case IPFIXOptionsTemplateRecord:
			records, err := DecodeOptionsDataSet(version, dataReader, templatec.Scopes, templatec.Options)
			if err != nil {
				return flowSet, &FlowError{version, "DataSet", obsDomainId, fsheader.Id, err}
			}

			datafs := OptionsDataFlowSet{
				FlowSetHeader: fsheader,
				Records:       records,
			}
			flowSet = datafs
		case NFv9OptionsTemplateRecord:
			records, err := DecodeOptionsDataSet(version, dataReader, templatec.Scopes, templatec.Options)
			if err != nil {
				return flowSet, &FlowError{version, "OptionDataSet", obsDomainId, fsheader.Id, err}
			}

			datafs := OptionsDataFlowSet{
				FlowSetHeader: fsheader,
				Records:       records,
			}
			flowSet = datafs
		}

	} else {
		return flowSet, &FlowError{version, "Decode", obsDomainId, fsheader.Id, fmt.Errorf("ID error")}
	}
	return flowSet, err
}

func DecodeMessageNetFlow(payload *bytes.Buffer, templates NetFlowTemplateSystem, packetNFv9 *NFv9Packet) error {
	packetNFv9.Version = 9
	if err := utils.BinaryDecoder(payload,
		&packetNFv9.Count,
		&packetNFv9.SystemUptime,
		&packetNFv9.UnixSeconds,
		&packetNFv9.SequenceNumber,
		&packetNFv9.SourceId,
	); err != nil {
		return &DecoderError{"NetFlowV9 header", err}
	}
	/*size = packetNFv9.Count
	packetNFv9.Version = version
	obsDomainId = packetNFv9.SourceId*/
	flowSets, err := DecodeMessageCommon(payload, templates, packetNFv9.SourceId, packetNFv9.Count, 9)
	packetNFv9.FlowSets = flowSets
	if err != nil {
		return &DecoderError{"NetFlowV9", err}
	}
	return nil
}

func DecodeMessageIPFIX(payload *bytes.Buffer, templates NetFlowTemplateSystem, packetIPFIX *IPFIXPacket) error {
	packetIPFIX.Version = 10
	if err := utils.BinaryDecoder(payload,
		&packetIPFIX.Length,
		&packetIPFIX.ExportTime,
		&packetIPFIX.SequenceNumber,
		&packetIPFIX.ObservationDomainId,
	); err != nil {
		return &DecoderError{"IPFIX header", err}
	}
	/*size = packetIPFIX.Length
	packetIPFIX.Version = version
	obsDomainId = packetIPFIX.ObservationDomainId*/
	flowSets, err := DecodeMessageCommon(payload, templates, packetIPFIX.ObservationDomainId, packetIPFIX.Length-16, 10)
	packetIPFIX.FlowSets = flowSets
	if err != nil {
		return &DecoderError{"IPFIX", err}
	}
	return nil
}

func DecodeMessageVersion(payload *bytes.Buffer, templates NetFlowTemplateSystem, packetNFv9 *NFv9Packet, packetIPFIX *IPFIXPacket) error {
	var version uint16

	if err := utils.BinaryDecoder(payload,
		&version,
	); err != nil {
		return &DecoderError{"IPFIX/NetFlowV9 version", err}
	}

	if version == 9 {
		if err := DecodeMessageNetFlow(payload, templates, packetNFv9); err != nil {
			return &DecoderError{"NetFlowV9", err}
		}
		return nil
	} else if version == 10 {
		if err := DecodeMessageIPFIX(payload, templates, packetIPFIX); err != nil {
			return &DecoderError{"IPFIX", err}
		}
		return nil
	}
	return &DecoderError{"IPFIX/NetFlowV9", fmt.Errorf("unknown version %d", version)}

}

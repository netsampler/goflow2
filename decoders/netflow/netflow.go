package netflow

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/netsampler/goflow2/decoders/netflow/templates"
	"github.com/netsampler/goflow2/decoders/utils"
)

type FlowBaseTemplateSet map[uint16]map[uint32]map[uint16]interface{}

type NetFlowTemplateSystem interface {
	GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error)
	AddTemplate(version uint16, obsDomainId uint32, template interface{})
}

// Transition structure to ease the conversion with the new template systems
type TemplateWrapper struct {
	Ctx   context.Context
	Key   string
	Inner templates.TemplateInterface
}

func (w *TemplateWrapper) getTemplateId(template interface{}) (templateId uint16) {
	switch templateIdConv := template.(type) {
	case IPFIXOptionsTemplateRecord:
		templateId = templateIdConv.TemplateId
	case NFv9OptionsTemplateRecord:
		templateId = templateIdConv.TemplateId
	case TemplateRecord:
		templateId = templateIdConv.TemplateId
	}
	return templateId
}

func (w TemplateWrapper) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	return w.Inner.GetTemplate(w.Ctx, &templates.TemplateKey{w.Key, version, obsDomainId, templateId})
}

func (w TemplateWrapper) AddTemplate(version uint16, obsDomainId uint32, template interface{}) {
	w.Inner.AddTemplate(w.Ctx, &templates.TemplateKey{w.Key, version, obsDomainId, w.getTemplateId(template)}, template)
}

func DecodeNFv9OptionsTemplateSet(payload *bytes.Buffer) ([]NFv9OptionsTemplateRecord, error) {
	var records []NFv9OptionsTemplateRecord
	var err error
	for payload.Len() >= 4 {
		optsTemplateRecord := NFv9OptionsTemplateRecord{}
		err = utils.BinaryDecoder(payload, &optsTemplateRecord.TemplateId, &optsTemplateRecord.ScopeLength, &optsTemplateRecord.OptionLength)
		if err != nil {
			return records, err
		}

		sizeScope := int(optsTemplateRecord.ScopeLength) / 4
		sizeOptions := int(optsTemplateRecord.OptionLength) / 4
		if sizeScope < 0 || sizeOptions < 0 {
			return records, fmt.Errorf("Error decoding OptionsTemplateSet: negative length.")
		}

		fields := make([]Field, sizeScope)
		for i := 0; i < sizeScope; i++ {
			field := Field{}
			if err := DecodeField(payload, &field, false); err != nil {
				return records, err
			}
			fields[i] = field
		}
		optsTemplateRecord.Scopes = fields

		fields = make([]Field, sizeOptions)
		for i := 0; i < sizeOptions; i++ {
			field := Field{}
			if err := DecodeField(payload, &field, false); err != nil {
				return records, err
			}
			fields[i] = field
		}
		optsTemplateRecord.Options = fields

		records = append(records, optsTemplateRecord)
	}

	return records, err
}

func DecodeField(payload *bytes.Buffer, field *Field, pen bool) error {
	err := utils.BinaryDecoder(payload, &field.Type, &field.Length)
	if pen && err == nil && field.Type&0x8000 != 0 {
		field.PenProvided = true
		err = utils.BinaryDecoder(payload, &field.Pen)
	}
	return err
}

func DecodeIPFIXOptionsTemplateSet(payload *bytes.Buffer) ([]IPFIXOptionsTemplateRecord, error) {
	var records []IPFIXOptionsTemplateRecord
	var err error
	for payload.Len() >= 4 {
		optsTemplateRecord := IPFIXOptionsTemplateRecord{}
		err = utils.BinaryDecoder(payload, &optsTemplateRecord.TemplateId, &optsTemplateRecord.FieldCount, &optsTemplateRecord.ScopeFieldCount)
		if err != nil {
			return records, err
		}

		fields := make([]Field, int(optsTemplateRecord.ScopeFieldCount))
		for i := 0; i < int(optsTemplateRecord.ScopeFieldCount); i++ {
			field := Field{}
			if err := DecodeField(payload, &field, true); err != nil {
				return records, err
			}
			fields[i] = field
		}
		optsTemplateRecord.Scopes = fields

		optionsSize := int(optsTemplateRecord.FieldCount) - int(optsTemplateRecord.ScopeFieldCount)
		if optionsSize < 0 {
			return records, fmt.Errorf("Error decoding OptionsTemplateSet: negative length.")
		}
		fields = make([]Field, optionsSize)
		for i := 0; i < optionsSize; i++ {
			field := Field{}
			if err := DecodeField(payload, &field, true); err != nil {
				return records, err
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
		err = utils.BinaryDecoder(payload, &templateRecord.TemplateId, &templateRecord.FieldCount)
		if err != nil {
			return records, err
		}

		if int(templateRecord.FieldCount) < 0 {
			return records, fmt.Errorf("Error decoding TemplateSet: zero count.")
		}

		fields := make([]Field, int(templateRecord.FieldCount))
		for i := 0; i < int(templateRecord.FieldCount); i++ {
			field := Field{}
			err := utils.BinaryDecoder(payload, &field.Type, &field.Length)
			if err == nil && version == 10 && field.Type&0x8000 != 0 {
				field.PenProvided = true
				field.Type = field.Type ^ 0x8000
				err = utils.BinaryDecoder(payload, &field.Pen)
			}
			if err != nil {
				return records, err
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

func DecodeDataSetUsingFields(version uint16, payload *bytes.Buffer, listFields []Field) []DataField {
	for payload.Len() >= GetTemplateSize(version, listFields) {

		dataFields := make([]DataField, len(listFields))
		for i, templateField := range listFields {

			finalLength := int(templateField.Length)
			if templateField.Length == 0xffff {
				var variableLen8 byte
				var variableLen16 uint16
				err := utils.BinaryDecoder(payload, &variableLen8)
				if err != nil {
					return []DataField{}
				}
				if variableLen8 == 0xff {
					err := utils.BinaryDecoder(payload, &variableLen16)
					if err != nil {
						return []DataField{}
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
		return dataFields
	}
	return []DataField{}
}

type ErrorTemplateNotFound struct {
	version      uint16
	obsDomainId  uint32
	templateId   uint16
	typeTemplate string
}

func NewErrorTemplateNotFound(version uint16, obsDomainId uint32, templateId uint16, typeTemplate string) *ErrorTemplateNotFound {
	return &ErrorTemplateNotFound{
		version:      version,
		obsDomainId:  obsDomainId,
		templateId:   templateId,
		typeTemplate: typeTemplate,
	}
}

func (e *ErrorTemplateNotFound) Error() string {
	return fmt.Sprintf("No %v template %v found for and domain id %v", e.typeTemplate, e.templateId, e.obsDomainId)
}

func DecodeOptionsDataSet(version uint16, payload *bytes.Buffer, listFieldsScopes, listFieldsOption []Field) ([]OptionsDataRecord, error) {
	var records []OptionsDataRecord

	listFieldsScopesSize := GetTemplateSize(version, listFieldsScopes)
	listFieldsOptionSize := GetTemplateSize(version, listFieldsOption)

	for payload.Len() >= listFieldsScopesSize+listFieldsOptionSize {
		scopeValues := DecodeDataSetUsingFields(version, payload, listFieldsScopes)
		optionValues := DecodeDataSetUsingFields(version, payload, listFieldsOption)

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
		values := DecodeDataSetUsingFields(version, payload, listFields)

		record := DataRecord{
			Values: values,
		}

		records = append(records, record)
	}
	return records, nil
}

func (ts *BasicTemplateSystem) GetTemplates() map[uint16]map[uint32]map[uint16]interface{} {
	ts.templateslock.RLock()
	tmp := ts.templates
	ts.templateslock.RUnlock()
	return tmp
}

func (ts *BasicTemplateSystem) AddTemplate(version uint16, obsDomainId uint32, template interface{}) {
	ts.templateslock.Lock()
	defer ts.templateslock.Unlock()
	_, exists := ts.templates[version]
	if exists != true {
		ts.templates[version] = make(map[uint32]map[uint16]interface{})
	}
	_, exists = ts.templates[version][obsDomainId]
	if exists != true {
		ts.templates[version][obsDomainId] = make(map[uint16]interface{})
	}
	var templateId uint16
	switch templateIdConv := template.(type) {
	case IPFIXOptionsTemplateRecord:
		templateId = templateIdConv.TemplateId
	case NFv9OptionsTemplateRecord:
		templateId = templateIdConv.TemplateId
	case TemplateRecord:
		templateId = templateIdConv.TemplateId
	}
	ts.templates[version][obsDomainId][templateId] = template
}

func (ts *BasicTemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	ts.templateslock.RLock()
	defer ts.templateslock.RUnlock()
	templatesVersion, okver := ts.templates[version]
	if okver {
		templatesObsDom, okobs := templatesVersion[obsDomainId]
		if okobs {
			template, okid := templatesObsDom[templateId]
			if okid {
				return template, nil
			}
		}
	}
	return nil, NewErrorTemplateNotFound(version, obsDomainId, templateId, "info")
}

type BasicTemplateSystem struct {
	templates     FlowBaseTemplateSet
	templateslock *sync.RWMutex
}

func CreateTemplateSystem() *BasicTemplateSystem {
	ts := &BasicTemplateSystem{
		templates:     make(FlowBaseTemplateSet),
		templateslock: &sync.RWMutex{},
	}
	return ts
}

func DecodeMessage(payload *bytes.Buffer, templates NetFlowTemplateSystem) (interface{}, error) {
	return DecodeMessageContext(context.Background(), payload, "", templates)
}

func DecodeMessageContext(ctx context.Context, payload *bytes.Buffer, templateKey string, tpli NetFlowTemplateSystem) (interface{}, error) {
	var size uint16
	packetNFv9 := NFv9Packet{}
	packetIPFIX := IPFIXPacket{}
	var returnItem interface{}

	var version uint16
	var obsDomainId uint32
	if err := utils.BinaryRead(payload, binary.BigEndian, &version); err != nil {
		return nil, fmt.Errorf("Error decoding version: %v", err)
	}

	if version == 9 {
		err := utils.BinaryDecoder(payload, &packetNFv9.Count, &packetNFv9.SystemUptime, &packetNFv9.UnixSeconds, &packetNFv9.SequenceNumber, &packetNFv9.SourceId)
		if err != nil {
			return nil, fmt.Errorf("Error decoding NetFlow v9 header: %v", err)
		}
		size = packetNFv9.Count
		packetNFv9.Version = version
		returnItem = *(&packetNFv9)
		obsDomainId = packetNFv9.SourceId
	} else if version == 10 {
		err := utils.BinaryDecoder(payload, &packetIPFIX.Length, &packetIPFIX.ExportTime, &packetIPFIX.SequenceNumber, &packetIPFIX.ObservationDomainId)
		if err != nil {
			return nil, fmt.Errorf("Error decoding IPFIX header: %v", err)
		}
		size = packetIPFIX.Length
		packetIPFIX.Version = version
		returnItem = *(&packetIPFIX)
		obsDomainId = packetIPFIX.ObservationDomainId
	} else {
		return nil, fmt.Errorf("NetFlow/IPFIX version error: %d", version)
	}
	read := 16
	startSize := payload.Len()

	for i := 0; ((i < int(size) && version == 9) || (uint16(read) < size && version == 10)) && payload.Len() > 0; i++ {
		fsheader := FlowSetHeader{}
		if err := utils.BinaryDecoder(payload, &fsheader.Id, &fsheader.Length); err != nil {
			return returnItem, fmt.Errorf("Error decoding FlowSet header: %v", err)
		}

		nextrelpos := int(fsheader.Length) - binary.Size(fsheader)
		if nextrelpos < 0 {
			return returnItem, fmt.Errorf("Error decoding packet: non-terminated stream")
		}

		var flowSet interface{}

		if fsheader.Id == 0 && version == 9 {
			templateReader := bytes.NewBuffer(payload.Next(nextrelpos))
			records, err := DecodeTemplateSet(version, templateReader)
			if err != nil {
				return returnItem, fmt.Errorf("Error decoding FlowSet header: %v", err)
			}
			templatefs := TemplateFlowSet{
				FlowSetHeader: fsheader,
				Records:       records,
			}

			flowSet = templatefs

			if tpli != nil {
				for _, record := range records {
					tpli.AddTemplate(version, obsDomainId, record)
					//tpli.AddTemplate(ctx, templates.NewTemplateKey(templateKey, version, obsDomainId, record.TemplateId), record)
				}
			}

		} else if fsheader.Id == 1 && version == 9 {
			templateReader := bytes.NewBuffer(payload.Next(nextrelpos))
			records, err := DecodeNFv9OptionsTemplateSet(templateReader)
			if err != nil {
				return returnItem, fmt.Errorf("Error decoding NetFlow OptionsTemplateSet: %v", err)
			}
			optsTemplatefs := NFv9OptionsTemplateFlowSet{
				FlowSetHeader: fsheader,
				Records:       records,
			}
			flowSet = optsTemplatefs

			if tpli != nil {
				for _, record := range records {
					tpli.AddTemplate(version, obsDomainId, record)
					//tpli.AddTemplate(ctx, templates.NewTemplateKey(templateKey, version, obsDomainId, record.TemplateId), record)
				}
			}

		} else if fsheader.Id == 2 && version == 10 {
			templateReader := bytes.NewBuffer(payload.Next(nextrelpos))
			records, err := DecodeTemplateSet(version, templateReader)
			if err != nil {
				return returnItem, fmt.Errorf("Error decoding IPFIX TemplateSet: %v", err)
			}
			templatefs := TemplateFlowSet{
				FlowSetHeader: fsheader,
				Records:       records,
			}
			flowSet = templatefs

			if tpli != nil {
				for _, record := range records {
					tpli.AddTemplate(version, obsDomainId, record)
					//tpli.AddTemplate(ctx, templates.NewTemplateKey(templateKey, version, obsDomainId, record.TemplateId), record)
				}
			}

		} else if fsheader.Id == 3 && version == 10 {
			templateReader := bytes.NewBuffer(payload.Next(nextrelpos))
			records, err := DecodeIPFIXOptionsTemplateSet(templateReader)
			if err != nil {
				return returnItem, fmt.Errorf("Error decoding IPFIX OptionsTemplateSet: %v", err)
			}
			optsTemplatefs := IPFIXOptionsTemplateFlowSet{
				FlowSetHeader: fsheader,
				Records:       records,
			}
			flowSet = optsTemplatefs

			if tpli != nil {
				for _, record := range records {
					tpli.AddTemplate(version, obsDomainId, record)
					//tpli.AddTemplate(ctx, templates.NewTemplateKey(templateKey, version, obsDomainId, record.TemplateId), record)
				}
			}

		} else if fsheader.Id >= 256 {
			dataReader := bytes.NewBuffer(payload.Next(nextrelpos))

			if tpli == nil {
				continue
			}

			template, err := tpli.GetTemplate(version, obsDomainId, fsheader.Id)
			//template, err := tpli.GetTemplate(ctx, templates.NewTemplateKey(templateKey, version, obsDomainId, fsheader.Id))

			if err == nil {
				switch templatec := template.(type) {
				case TemplateRecord:
					records, err := DecodeDataSet(version, dataReader, templatec.Fields)
					if err != nil {
						return returnItem, fmt.Errorf("Error decoding DataSet: %v", err)
					}
					datafs := DataFlowSet{
						FlowSetHeader: fsheader,
						Records:       records,
					}
					flowSet = datafs
				case IPFIXOptionsTemplateRecord:
					records, err := DecodeOptionsDataSet(version, dataReader, templatec.Scopes, templatec.Options)
					if err != nil {
						return returnItem, fmt.Errorf("Error decoding DataSet: %v", err)
					}

					datafs := OptionsDataFlowSet{
						FlowSetHeader: fsheader,
						Records:       records,
					}
					flowSet = datafs
				case NFv9OptionsTemplateRecord:
					records, err := DecodeOptionsDataSet(version, dataReader, templatec.Scopes, templatec.Options)
					if err != nil {
						return returnItem, fmt.Errorf("Error decoding OptionDataSet: %v", err)
					}

					datafs := OptionsDataFlowSet{
						FlowSetHeader: fsheader,
						Records:       records,
					}
					flowSet = datafs
				}
			} else {
				return returnItem, err
			}
		} else {
			return returnItem, fmt.Errorf("Error with ID %d", fsheader.Id)
		}

		if version == 9 && flowSet != nil {
			packetNFv9.FlowSets = append(packetNFv9.FlowSets, flowSet)
		} else if version == 10 && flowSet != nil {
			packetIPFIX.FlowSets = append(packetIPFIX.FlowSets, flowSet)
		}
		read = startSize - payload.Len() + 16
	}

	if version == 9 {
		return packetNFv9, nil
	} else if version == 10 {
		return packetIPFIX, nil
	} else {
		return returnItem, fmt.Errorf("Unknown version: %d", version)
	}
}

// Package netflow decodes NetFlow v9 and IPFIX packets.
package netflow

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/netsampler/goflow2/v3/decoders/utils"
)

// DecoderError wraps a NetFlow decode error with its decoder name.
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

// FlowError annotates an error with flow metadata.
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

// DecodeNFv9OptionsTemplateSet decodes a v9 options template flow set.
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
			return records, fmt.Errorf("NFv9OptionsTemplateSet: header [%w]", err)
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

	return records, nil
}

// DecodeField decodes a field and optional enterprise number.
func DecodeField(payload *bytes.Buffer, field *Field, pen bool) error {
	if err := utils.BinaryDecoder(payload,
		&field.Type,
		&field.Length,
	); err != nil {
		return fmt.Errorf("DecodeField: header [%w]", err)
	}
	if pen && field.Type&0x8000 != 0 {
		field.PenProvided = true
		if err := utils.BinaryDecoder(payload,
			&field.Pen,
		); err != nil {
			return fmt.Errorf("DecodeField: pen [%w]", err)
		}
		return nil
	}
	return nil
}

// DecodeIPFIXOptionsTemplateSet decodes an IPFIX options template flow set.
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

// DecodeTemplateSet decodes a template flow set for NetFlow v9 or IPFIX.
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

// GetTemplateSize returns the total byte length of a template's fixed fields.
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

// minRecordSize returns the smallest number of bytes a data record for the
// template can occupy: the fixed fields plus one length byte per
// variable-length field.
func minRecordSize(version uint16, template []Field) int {
	size := GetTemplateSize(version, template)
	for _, templateField := range template {
		if templateField.Length == 0xffff {
			size++
		}
	}
	return size
}

// maxPreallocatedRecords bounds the arena reserved for one data set, since the
// record count is estimated from the payload length.
const maxPreallocatedRecords = 1024

// estimateRecords returns an upper bound for the number of records of the
// template that fit in payloadLen bytes, capped to keep preallocation small.
func estimateRecords(payloadLen, recordSize int) int {
	if recordSize <= 0 {
		return 0
	}
	n := payloadLen / recordSize
	if n > maxPreallocatedRecords {
		n = maxPreallocatedRecords
	}
	return n
}

// DecodeDataSetUsingFields decodes one data record described by listFields.
// When the payload is shorter than the template's fixed size the returned
// fields are zero values.
func DecodeDataSetUsingFields(version uint16, payload *bytes.Buffer, listFields []Field) ([]DataField, error) {
	return appendDataFields(nil, version, payload, listFields)
}

// appendDataFields decodes one data record and appends its fields to dst,
// which lets the caller share one backing array across the records of a set.
func appendDataFields(dst []DataField, version uint16, payload *bytes.Buffer, listFields []Field) ([]DataField, error) {
	if payload.Len() < GetTemplateSize(version, listFields) {
		return append(dst, make([]DataField, len(listFields))...), nil
	}

	for _, templateField := range listFields {
		finalLength := int(templateField.Length)
		if templateField.Length == 0xffff {
			variableLen8, err := payload.ReadByte()
			if err != nil {
				return dst, fmt.Errorf("DataSet: variable length header [%w]", err)
			}
			if variableLen8 == 0xff {
				hi, err := payload.ReadByte()
				if err != nil {
					return dst, fmt.Errorf("DataSet: extended variable length [%w]", err)
				}
				lo, err := payload.ReadByte()
				if err != nil {
					return dst, fmt.Errorf("DataSet: extended variable length [%w]", err)
				}
				finalLength = int(hi)<<8 | int(lo)
			} else {
				finalLength = int(variableLen8)
			}
		}

		dst = append(dst, DataField{
			Type:        templateField.Type,
			PenProvided: templateField.PenProvided,
			Pen:         templateField.Pen,
			Value:       payload.Next(finalLength),
		})
	}
	return dst, nil
}

// sliceRecord returns the fields appended since start as an independent
// slice: its capacity is clipped so that appending to it never writes into
// the shared arena.
func sliceRecord(arena []DataField, start int) []DataField {
	return arena[start:len(arena):len(arena)]
}

func DecodeOptionsDataSet(version uint16, payload *bytes.Buffer, listFieldsScopes, listFieldsOption []Field) ([]OptionsDataRecord, error) {
	var records []OptionsDataRecord

	// A record needs at least its fixed fields plus one length byte per
	// variable-length field; requiring that much also guarantees progress.
	recordSize := minRecordSize(version, listFieldsScopes) + minRecordSize(version, listFieldsOption)
	if recordSize == 0 {
		if payload.Len() > 0 {
			return records, fmt.Errorf("OptionsDataSet: template without fields")
		}
		return records, nil
	}

	fieldsPerRecord := len(listFieldsScopes) + len(listFieldsOption)
	estimate := estimateRecords(payload.Len(), recordSize)
	arena := make([]DataField, 0, estimate*fieldsPerRecord)
	records = make([]OptionsDataRecord, 0, estimate)

	for payload.Len() >= recordSize {
		var err error
		start := len(arena)
		if arena, err = appendDataFields(arena, version, payload, listFieldsScopes); err != nil {
			return records, fmt.Errorf("OptionsDataSet: scope [%w]", err)
		}
		scopeValues := sliceRecord(arena, start)

		start = len(arena)
		if arena, err = appendDataFields(arena, version, payload, listFieldsOption); err != nil {
			return records, fmt.Errorf("OptionsDataSet: options [%w]", err)
		}
		optionValues := sliceRecord(arena, start)

		records = append(records, OptionsDataRecord{
			ScopesValues:  scopeValues,
			OptionsValues: optionValues,
		})
	}
	return records, nil
}

func DecodeDataSet(version uint16, payload *bytes.Buffer, listFields []Field) ([]DataRecord, error) {
	var records []DataRecord

	// A record needs at least its fixed fields plus one length byte per
	// variable-length field; requiring that much also guarantees progress.
	recordSize := minRecordSize(version, listFields)
	if recordSize == 0 {
		if payload.Len() > 0 {
			return records, fmt.Errorf("DataSet: template without fields")
		}
		return records, nil
	}

	// All records of the set share one backing array of fields, sized from
	// the payload length instead of allocating one slice per record.
	estimate := estimateRecords(payload.Len(), recordSize)
	arena := make([]DataField, 0, estimate*len(listFields))
	records = make([]DataRecord, 0, estimate)

	for payload.Len() >= recordSize {
		var err error
		start := len(arena)
		if arena, err = appendDataFields(arena, version, payload, listFields); err != nil {
			return records, fmt.Errorf("DataSet: fields [%w]", err)
		}
		records = append(records, DataRecord{
			Values: sliceRecord(arena, start),
		})
	}
	return records, nil
}

func DecodeMessageCommon(payload *bytes.Buffer, store TemplateStore, ctx FlowContext, obsDomainId uint32, size, version uint16) (flowSets []interface{}, err error) {
	var read int
	startSize := payload.Len()
	headerSize := binary.Size(FlowSetHeader{})
	for i := 0; payload.Len() >= headerSize && (version == 9 || uint16(read) < size); i++ {
		if flowSet, lerr := DecodeMessageCommonFlowSet(payload, store, ctx, obsDomainId, version); lerr != nil && !errors.Is(lerr, ErrorTemplateNotFound) {
			return flowSets, fmt.Errorf("DecodeMessageCommon: %w", lerr)
		} else {
			flowSets = append(flowSets, flowSet)
			if lerr != nil {
				err = errors.Join(err, lerr)
			}
		}
		read = startSize - payload.Len()
	}
	if err != nil {
		return flowSets, fmt.Errorf("DecodeMessageCommon: %w", err)
	}
	return flowSets, nil
}

func DecodeMessageCommonFlowSet(payload *bytes.Buffer, store TemplateStore, ctx FlowContext, obsDomainId uint32, version uint16) (flowSet interface{}, err error) {
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

		if store != nil {
			for _, record := range records {
				if _, err := store.AddTemplate(ctx, version, obsDomainId, record.TemplateId, record); err != nil {
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

		if store != nil {
			for _, record := range records {
				if _, err := store.AddTemplate(ctx, version, obsDomainId, record.TemplateId, record); err != nil {
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

		if store != nil {
			for _, record := range records {
				if _, err := store.AddTemplate(ctx, version, obsDomainId, record.TemplateId, record); err != nil {
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

		if store != nil {
			for _, record := range records {
				if _, err := store.AddTemplate(ctx, version, obsDomainId, record.TemplateId, record); err != nil {
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

		if store == nil {
			return flowSet, &FlowError{version, "Templates", obsDomainId, fsheader.Id, fmt.Errorf("no templates")}
		}

		template, err := store.GetTemplate(ctx, version, obsDomainId, fsheader.Id)
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
	if err != nil {
		return flowSet, fmt.Errorf("FlowSet decode: %w", err)
	}
	return flowSet, nil
}

func DecodeMessageNetFlow(payload *bytes.Buffer, store TemplateStore, ctx FlowContext, packetNFv9 *NFv9Packet) error {
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
	flowSets, err := DecodeMessageCommon(payload, store, ctx, packetNFv9.SourceId, packetNFv9.Count, 9)
	packetNFv9.FlowSets = flowSets
	if err != nil {
		return &DecoderError{"NetFlowV9", err}
	}
	return nil
}

func DecodeMessageIPFIX(payload *bytes.Buffer, store TemplateStore, ctx FlowContext, packetIPFIX *IPFIXPacket) error {
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
	flowSets, err := DecodeMessageCommon(payload, store, ctx, packetIPFIX.ObservationDomainId, packetIPFIX.Length-16, 10)
	packetIPFIX.FlowSets = flowSets
	if err != nil {
		return &DecoderError{"IPFIX", err}
	}
	return nil
}

func DecodeMessageVersion(payload *bytes.Buffer, store TemplateStore, ctx FlowContext, packetNFv9 *NFv9Packet, packetIPFIX *IPFIXPacket) error {
	var version uint16

	if err := utils.BinaryDecoder(payload,
		&version,
	); err != nil {
		return &DecoderError{"IPFIX/NetFlowV9 version", err}
	}

	switch version {
	case 9:
		if err := DecodeMessageNetFlow(payload, store, ctx, packetNFv9); err != nil {
			return &DecoderError{"NetFlowV9", err}
		}
		return nil
	case 10:
		if err := DecodeMessageIPFIX(payload, store, ctx, packetIPFIX); err != nil {
			return &DecoderError{"IPFIX", err}
		}
		return nil
	}
	return &DecoderError{"IPFIX/NetFlowV9", fmt.Errorf("unknown version %d", version)}

}

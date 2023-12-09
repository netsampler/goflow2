package netflow

import (
	"fmt"
)

// FlowSetHeader contains fields shared by all Flow Sets (DataFlowSet,
// TemplateFlowSet, OptionsTemplateFlowSet).
type FlowSetHeader struct {
	// FlowSet ID:
	//    0 for TemplateFlowSet
	//    1 for OptionsTemplateFlowSet
	//    256-65535 for DataFlowSet (used as TemplateId)
	Id uint16 `json:"id"`

	// The total length of this FlowSet in bytes (including padding).
	Length uint16 `json:"length"`
}

// TemplateFlowSet is a collection of templates that describe structure of Data
// Records (actual NetFlow data).
type TemplateFlowSet struct {
	FlowSetHeader

	// List of Template Records
	Records []TemplateRecord `json:"records"`
}

// DataFlowSet is a collection of Data Records (actual NetFlow data) and Options
// Data Records (meta data).
type DataFlowSet struct {
	FlowSetHeader

	Records []DataRecord `json:"records"`
}

// RawFlowSet is a a set that could not be decoded due to the absence of a template
type RawFlowSet struct {
	FlowSetHeader

	Records []byte `json:"records"`
}

type OptionsDataFlowSet struct {
	FlowSetHeader

	Records []OptionsDataRecord `json:"records"`
}

// TemplateRecord is a single template that describes structure of a Flow Record
// (actual Netflow data).
type TemplateRecord struct {
	// Each of the newly generated Template Records is given a unique
	// Template ID. This uniqueness is local to the Observation Domain that
	// generated the Template ID. Template IDs of Data FlowSets are numbered
	// from 256 to 65535.
	TemplateId uint16 `json:"template-id"`

	// Number of fields in this Template Record. Because a Template FlowSet
	// usually contains multiple Template Records, this field allows the
	// Collector to determine the end of the current Template Record and
	// the start of the next.
	FieldCount uint16 `json:"field-count"`

	// List of fields in this Template Record.
	Fields []Field `json:"fields"`
}

type DataRecord struct {
	Values []DataField `json:"values"`
}

// OptionsDataRecord is meta data sent alongide actual NetFlow data. Combined
// with OptionsTemplateRecord it can be decoded to a single data row.
type OptionsDataRecord struct {
	// List of Scope values stored in raw format as []byte
	ScopesValues []DataField `json:"scope-values"`

	// List of Optons values stored in raw format as []byte
	OptionsValues []DataField `json:"option-values"`
}

// Field describes type and length of a single value in a Flow Data Record.
// Field does not contain the record value itself it is just a description of
// what record value will look like.
type Field struct {
	// A numeric value that represents the type of field.
	PenProvided bool   `json:"pen-provided"`
	Type        uint16 `json:"type"`

	// The length (in bytes) of the field.
	Length uint16 `json:"length"`

	Pen uint32 `json:"pen"`
}

type DataField struct {
	// A numeric value that represents the type of field.
	PenProvided bool   `json:"pen-provided"`
	Type        uint16 `json:"type"`
	Pen         uint32 `json:"pen"`

	// The value (in bytes) of the field.
	Value interface{} `json:"value"`
	//Value []byte
}

func (flowSet RawFlowSet) String() string {
	str := fmt.Sprintf("       Id %v\n", flowSet.Id)
	str += fmt.Sprintf("       Length: %v\n", len(flowSet.Records))
	str += fmt.Sprintf("       Records: %v\n", flowSet.Records)

	return str
}

func (flowSet OptionsDataFlowSet) String(TypeToString func(uint16) string, ScopeToString func(uint16) string) string {
	str := fmt.Sprintf("       Id %v\n", flowSet.Id)
	str += fmt.Sprintf("       Length: %v\n", flowSet.Length)
	str += fmt.Sprintf("       Records (%v records):\n", len(flowSet.Records))

	for j, record := range flowSet.Records {
		str += fmt.Sprintf("       - Record %v:\n", j)
		str += fmt.Sprintf("            Scopes (%v):\n", len(record.ScopesValues))

		for k, value := range record.ScopesValues {
			str += fmt.Sprintf("            - %v. %v (%v): %v\n", k, ScopeToString(value.Type), value.Type, value.Value)
		}

		str += fmt.Sprintf("            Options (%v):\n", len(record.OptionsValues))

		for k, value := range record.OptionsValues {
			str += fmt.Sprintf("            - %v. %v (%v): %v\n", k, TypeToString(value.Type), value.Type, value.Value)
		}
	}

	return str
}

func (flowSet DataFlowSet) String(TypeToString func(uint16) string) string {
	str := fmt.Sprintf("       Id %v\n", flowSet.Id)
	str += fmt.Sprintf("       Length: %v\n", flowSet.Length)
	str += fmt.Sprintf("       Records (%v records):\n", len(flowSet.Records))

	for j, record := range flowSet.Records {
		str += fmt.Sprintf("       - Record %v:\n", j)
		str += fmt.Sprintf("            Values (%v):\n", len(record.Values))

		for k, value := range record.Values {
			str += fmt.Sprintf("            - %v. %v (%v): %v\n", k, TypeToString(value.Type), value.Type, value.Value)
		}
	}

	return str
}

func (flowSet TemplateFlowSet) String(TypeToString func(uint16) string) string {
	str := fmt.Sprintf("       Id %v\n", flowSet.Id)
	str += fmt.Sprintf("       Length: %v\n", flowSet.Length)
	str += fmt.Sprintf("       Records (%v records):\n", len(flowSet.Records))

	for j, record := range flowSet.Records {
		str += fmt.Sprintf("       - %v. Record:\n", j)
		str += fmt.Sprintf("            TemplateId: %v\n", record.TemplateId)
		str += fmt.Sprintf("            FieldCount: %v\n", record.FieldCount)
		str += fmt.Sprintf("            Fields (%v):\n", len(record.Fields))

		for k, field := range record.Fields {
			str += fmt.Sprintf("            - %v. %v (%v/%v): %v\n", k, TypeToString(field.Type), field.Type, field.PenProvided, field.Length)
		}
	}

	return str
}

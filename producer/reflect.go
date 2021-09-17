package producer

import (
	"fmt"
	"reflect"

	"github.com/netsampler/goflow2/decoders/netflow"
	flowmessage "github.com/netsampler/goflow2/pb"
)

func MapCustom(flowMessage *flowmessage.FlowMessage, df netflow.DataField, mapper *NetFlowMapper) {
	mapped, ok := mapper.Map(df)
	if ok {
		vfm := reflect.ValueOf(flowMessage)
		vfm = reflect.Indirect(vfm)

		fieldValue := vfm.FieldByName(mapped.Destination)
		if fieldValue.IsValid() {
			typeDest := fieldValue.Type()
			fieldValueAddr := fieldValue.Addr()
			v := df.Value.([]byte)
			if typeDest.Kind() == reflect.Slice && typeDest.Elem().Kind() == reflect.Uint8 {
				fieldValue.SetBytes(v)
			} else if fieldValueAddr.IsValid() && (typeDest.Kind() == reflect.Uint8 || typeDest.Kind() == reflect.Uint16 || typeDest.Kind() == reflect.Uint32 || typeDest.Kind() == reflect.Uint64) {
				DecodeUNumber(v, fieldValueAddr.Interface())
			} else if fieldValueAddr.IsValid() && (typeDest.Kind() == reflect.Int8 || typeDest.Kind() == reflect.Int16 || typeDest.Kind() == reflect.Int32 || typeDest.Kind() == reflect.Int64) {
				DecodeNumber(v, fieldValueAddr.Interface())
			}
		}
	}
}

type NetFlowMapField struct {
	PenProvided bool
	Type        uint16 `json:"field"`
	Pen         uint32 `json:"pen"`

	Destination       string `json:"destination"`
	DestinationLength uint8  `json:"dlen"`
}

type IPFIXProducerConfig struct {
	SkipBase bool              `json:"skip-base"`
	Mapping  []NetFlowMapField `json:"mapping"`
}

type NetFlowV9ProducerConfig struct {
	SkipBase bool              `json:"skip-base"`
	Mapping  []NetFlowMapField `json:"mapping"`
}

type NetFlowProducerConfig struct {
	IPFIX     IPFIXProducerConfig     `json:"ipfix"`
	NetFlowV9 NetFlowV9ProducerConfig `json:"netflowv9"`
}

type DataMap struct {
	Destination string
}

type NetFlowMapper struct {
	data map[string]DataMap
}

func (m *NetFlowMapper) Map(field netflow.DataField) (DataMap, bool) {
	mapped, found := m.data[fmt.Sprintf("%v-%d-%d", field.PenProvided, field.Pen, field.Type)]
	return mapped, found
}

func MapFields(fields []NetFlowMapField) *NetFlowMapper {
	ret := make(map[string]DataMap)
	for _, field := range fields {
		ret[fmt.Sprintf("%v-%d-%d", field.PenProvided, field.Pen, field.Type)] = DataMap{Destination: field.Destination}
	}
	return &NetFlowMapper{ret}
}

package protoproducer

import (
	"fmt"
	"reflect"
)

func mapFieldsSFlow(fields []SFlowMapField) *SFlowMapper {
	ret := make(map[string][]DataMapLayer)
	for _, field := range fields {
		retLayerEntry := DataMapLayer{
			Offset: field.Offset,
			Length: field.Length,
		}
		retLayerEntry.Destination = field.Destination
		retLayerEntry.Endianness = field.Endian
		retLayer := ret[field.Layer]
		retLayer = append(retLayer, retLayerEntry)
		ret[field.Layer] = retLayer
	}
	return &SFlowMapper{ret}
}

func mapFieldsNetFlow(fields []NetFlowMapField) *NetFlowMapper {
	ret := make(map[string]DataMap)
	for _, field := range fields {
		dm := DataMap{}
		dm.Destination = field.Destination
		dm.Endianness = field.Endian
		ret[fmt.Sprintf("%v-%d-%d", field.PenProvided, field.Pen, field.Type)] = dm
	}
	return &NetFlowMapper{ret}
}

func (c *producerConfigMapped) finalizemapDest(v *MapConfigBase) error {
	if vv, ok := c.Formatter.pbMap[v.Destination]; ok {
		v.ProtoIndex = vv.Index

		if pt, ok := ProtoTypeMap[vv.Type]; ok {
			v.ProtoType = pt
		} else {
			return fmt.Errorf("could not map %s to a ProtoType", vv.Type)
		}

		v.ProtoArray = vv.Array
	}
	return nil
}

func (c *producerConfigMapped) finalizeSFlowMapper(m *SFlowMapper) error {
	if m == nil {
		return nil
	}
	for k, vlist := range m.data {
		for i, v := range vlist {
			if err := c.finalizemapDest(&(v.MapConfigBase)); err != nil {
				return err
			}
			m.data[k][i] = v
		}

	}
	return nil
}

func (c *producerConfigMapped) finalizeNetFlowMapper(m *NetFlowMapper) error {
	if m == nil {
		return nil
	}
	for k, v := range m.data {
		if err := c.finalizemapDest(&(v.MapConfigBase)); err != nil {
			return err
		}
		m.data[k] = v
	}
	return nil
}

func (c *producerConfigMapped) finalize() error {
	if c.Formatter == nil {
		return nil
	}
	if err := c.finalizeNetFlowMapper(c.IPFIX); err != nil {
		return err
	}
	if err := c.finalizeNetFlowMapper(c.NetFlowV9); err != nil {
		return err
	}
	if err := c.finalizeSFlowMapper(c.SFlow); err != nil {
		return err
	}

	return nil
}

func mapFormat(cfg *ProducerConfig) (*FormatterConfigMapper, error) {
	formatterMapped := &FormatterConfigMapper{}

	selectorTag := "json"
	var msg ProtoProducerMessage
	msgT := reflect.TypeOf(&msg.FlowMessage).Elem() // required indirect otherwise go vet indicates TypeOf copies lock
	reMap := make(map[string]string)
	numToPb := make(map[int32]ProtobufFormatterConfig)
	var fields []string

	for i := 0; i < msgT.NumField(); i++ {
		field := msgT.Field(i)
		if !field.IsExported() {
			continue
		}
		fieldName := field.Name
		if selectorTag != "" {
			fieldName = ExtractTag(selectorTag, fieldName, field.Tag)
			reMap[fieldName] = field.Name
			fields = append(fields, fieldName)
		}
		//customSelectorTmp[i] = fieldName

	}

	formatterMapped.reMap = reMap
	pbMap := make(map[string]ProtobufFormatterConfig)
	formatterMapped.render = make(map[string]RenderFunc)
	formatterMapped.rename = make(map[string]string)
	formatterMapped.isSlice = map[string]bool{
		"BgpCommunities": true,
		"AsPath":         true,
		"MplsIp":         true,
		"MplsLabel":      true,
		"MplsTtl":        true,
	} // todo: improve this with defaults
	for k, v := range defaultRenderers {
		formatterMapped.render[k] = v
	}

	if cfg != nil {
		cfgFormatter := cfg.Formatter

		// manual protobuf fields to add
		for _, pbField := range cfgFormatter.Protobuf {
			reMap[pbField.Name] = ""      // special dynamic protobuf
			pbMap[pbField.Name] = pbField // todo: check if type is valid

			numToPb[pbField.Index] = pbField
			formatterMapped.isSlice[pbField.Name] = pbField.Array
		}
		// populate manual renames
		for k, v := range cfgFormatter.Rename {
			formatterMapped.rename[k] = v
		}

		// populate key
		for _, v := range cfgFormatter.Key {
			if _, ok := reMap[v]; !ok {
				return formatterMapped, fmt.Errorf("key field %s does not exist", v)
			}
			formatterMapped.key = append(formatterMapped.key, v)
		}

		// process renderers
		for k, v := range cfgFormatter.Render {
			if kk, ok := reMap[k]; ok && kk != "" {
				k = kk
			}
			if renderer, ok := renderers[v]; ok {
				formatterMapped.render[k] = renderer
			} else {
				return formatterMapped, fmt.Errorf("field %s is not a renderer", v) // todo: make proper error
			}
		}

		// if the config does not contain any fields initially, we set with the protobuf ones
		if len(cfgFormatter.Fields) == 0 {
			formatterMapped.fields = fields
		} else {
			for _, field := range cfgFormatter.Fields {
				if _, ok := reMap[field]; !ok {

					// check if it's a virtual field
					if _, ok := formatterMapped.render[field]; !ok {
						return formatterMapped, fmt.Errorf("field %s in config not found in protobuf", field) // todo: make proper error
					}

				}
			}
			formatterMapped.fields = cfgFormatter.Fields
		}

		formatterMapped.pbMap = pbMap
		formatterMapped.numToPb = numToPb

	} else {
		formatterMapped.fields = fields
	}

	return formatterMapped, nil
}

func mapConfig(cfg *ProducerConfig) (*producerConfigMapped, error) {
	newCfg := &producerConfigMapped{}
	if cfg != nil {
		newCfg.IPFIX = mapFieldsNetFlow(cfg.IPFIX.Mapping)
		newCfg.NetFlowV9 = mapFieldsNetFlow(cfg.NetFlowV9.Mapping)
		newCfg.SFlow = mapFieldsSFlow(cfg.SFlow.Mapping)
	}
	var err error
	if newCfg.Formatter, err = mapFormat(cfg); err != nil {
		return newCfg, err
	}
	return newCfg, newCfg.finalize()
}

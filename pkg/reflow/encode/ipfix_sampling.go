package encode

import (
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
)

const ipfixSamplingTemplateID = 65535

func ipfixSelectorCatalog(catalog map[string]config.IPFIXFieldDefinition) map[string]config.IPFIXFieldDefinition {
	out := make(map[string]config.IPFIXFieldDefinition, len(catalog)+1)
	maps.Copy(out, catalog)
	// A uniform selector's divisor is a stable ID in the observation domain.
	// selectorId is unsigned64 and permits reduced-size encoding.
	out["sampling_rate"] = config.IPFIXFieldDefinition{
		ID: netflow.IPFIX_FIELD_selectorId, Length: 4, Type: "unsigned32",
	}
	return out
}

func (e *IPFIXEncoder) checkSamplingTemplateID(id uint16) error {
	if e.cfg.TemplatedFlow.SamplingMode == config.IPFIXSamplingSelector && id == ipfixSamplingTemplateID {
		return fmt.Errorf("IPFIX template ID %d is reserved for sampling selector Options", id)
	}
	return nil
}

func (e *IPFIXEncoder) checkSamplingDatagramSize(data []byte) error {
	if limit := e.cfg.MaxDatagramBytes; e.cfg.TemplatedFlow.SamplingMode == config.IPFIXSamplingSelector && limit > 0 && len(data) > limit {
		return fmt.Errorf("IPFIX record with selector Options is %d bytes, exceeds max_datagram_bytes %d", len(data), limit)
	}
	return nil
}

// Send the mapping with the data that uses it so a lost or reordered UDP
// datagram cannot separate a record from its sampling settings.
func (e *IPFIXEncoder) addSamplingOptions(packet *netflow.IPFIXPacket) error {
	if e.cfg.TemplatedFlow.SamplingMode != config.IPFIXSamplingSelector {
		return nil
	}
	selectors := make(map[uint32]struct{})
	for _, set := range packet.FlowSets {
		switch set := set.(type) {
		case netflow.DataFlowSet:
			if err := e.checkSamplingTemplateID(set.Id); err != nil {
				return err
			}
			for _, record := range set.Records {
				addIPFIXSelectorIDs(selectors, record.Values)
			}
		case netflow.OptionsDataFlowSet:
			if err := e.checkSamplingTemplateID(set.Id); err != nil {
				return err
			}
			for _, record := range set.Records {
				addIPFIXSelectorIDs(selectors, record.ScopesValues)
				addIPFIXSelectorIDs(selectors, record.OptionsValues)
			}
		case netflow.TemplateFlowSet:
			for _, record := range set.Records {
				if err := e.checkSamplingTemplateID(record.TemplateId); err != nil {
					return err
				}
			}
		case netflow.IPFIXOptionsTemplateFlowSet:
			for _, record := range set.Records {
				if err := e.checkSamplingTemplateID(record.TemplateId); err != nil {
					return err
				}
			}
		}
	}
	if len(selectors) == 0 {
		return nil
	}

	records := make([]netflow.OptionsDataRecord, 0, len(selectors))
	for _, id := range slices.Sorted(maps.Keys(selectors)) {
		records = append(records, netflow.OptionsDataRecord{
			ScopesValues: []netflow.DataField{
				{Type: netflow.IPFIX_FIELD_selectorId, Value: encodeU32(id)},
			},
			OptionsValues: []netflow.DataField{
				{Type: netflow.IPFIX_FIELD_selectorAlgorithm, Value: []byte{0, 4}},
				{Type: netflow.IPFIX_FIELD_samplingProbability, Value: encodeU64(math.Float64bits(1 / float64(id)))},
			},
		})
	}
	packet.FlowSets = append([]interface{}{
		netflow.IPFIXOptionsTemplateFlowSet{
			FlowSetHeader: netflow.FlowSetHeader{Id: 3},
			Records: []netflow.IPFIXOptionsTemplateRecord{{
				TemplateId:      ipfixSamplingTemplateID,
				FieldCount:      3,
				ScopeFieldCount: 1,
				Scopes:          []netflow.Field{{Type: netflow.IPFIX_FIELD_selectorId, Length: 4}},
				Options: []netflow.Field{
					{Type: netflow.IPFIX_FIELD_selectorAlgorithm, Length: 2},
					{Type: netflow.IPFIX_FIELD_samplingProbability, Length: 8},
				},
			}},
		},
		netflow.OptionsDataFlowSet{
			FlowSetHeader: netflow.FlowSetHeader{Id: ipfixSamplingTemplateID},
			Records:       records,
		},
	}, packet.FlowSets...)
	return nil
}

func addIPFIXSelectorIDs(selectors map[uint32]struct{}, fields []netflow.DataField) {
	for _, field := range fields {
		if field.Type == netflow.IPFIX_FIELD_selectorId && !field.PenProvided {
			if value, ok := field.Value.([]byte); ok && len(value) == 4 {
				if id := binary.BigEndian.Uint32(value); id != 0 {
					selectors[id] = struct{}{}
				}
			}
		}
	}
}

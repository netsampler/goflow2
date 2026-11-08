package netflow

import (
	"encoding/binary"
	"math"
)

// IPFIXSamplingSelectors maps uniform packet selectors to integral divisors.
type IPFIXSamplingSelectors map[uint64]uint32

// IPFIXSelectorsFromOptions reads the selector definitions in an IPFIX message.
func IPFIXSelectorsFromOptions(sets []OptionsDataFlowSet) IPFIXSamplingSelectors {
	var rates IPFIXSamplingSelectors
	for _, set := range sets {
		for _, record := range set.Records {
			if len(record.ScopesValues) != 1 || record.ScopesValues[0].Type != IPFIX_FIELD_selectorId || record.ScopesValues[0].PenProvided {
				continue
			}
			id, _ := record.ScopesValues[0].Value.([]byte)
			algorithm := ipfixFieldBytes(record.OptionsValues, IPFIX_FIELD_selectorAlgorithm)
			if len(id) == 0 || len(id) > 8 || len(algorithm) > 2 || ipfixUint(algorithm) != 4 {
				continue
			}
			raw := ipfixFieldBytes(record.OptionsValues, IPFIX_FIELD_samplingProbability)
			if len(raw) != 8 {
				continue
			}
			probability := math.Float64frombits(binary.BigEndian.Uint64(raw))
			rate := math.Round(1 / probability)
			if rate >= 1 && rate <= math.MaxUint32 && probability == 1/rate {
				if rates == nil {
					rates = make(IPFIXSamplingSelectors)
				}
				rates[ipfixUint(id)] = uint32(rate)
			}
		}
	}
	return rates
}

// Rate returns the divisor for the selector referenced by a flow record.
func (s IPFIXSamplingSelectors) Rate(fields []DataField) uint32 {
	if len(s) != 0 {
		id := ipfixFieldBytes(fields, IPFIX_FIELD_selectorId)
		if len(id) > 0 && len(id) <= 8 {
			return s[ipfixUint(id)]
		}
	}
	return 0
}

func ipfixFieldBytes(fields []DataField, id uint16) []byte {
	for _, field := range fields {
		if field.Type == id && !field.PenProvided {
			value, _ := field.Value.([]byte)
			return value
		}
	}
	return nil
}

func ipfixUint(raw []byte) uint64 {
	var value uint64
	for _, b := range raw {
		value = value<<8 | uint64(b)
	}
	return value
}

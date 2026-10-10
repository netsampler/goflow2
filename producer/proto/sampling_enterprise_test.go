package protoproducer

import (
	"fmt"
	"testing"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/stretchr/testify/require"
)

func TestSamplingOptionsIgnoreEnterpriseElements(t *testing.T) {
	for _, id := range []uint16{34, 50, 305} {
		for _, pen := range []uint32{0, 9} {
			for _, withStandard := range []bool{false, true} {
				t.Run(fmt.Sprintf("id_%d/pen_%d/standard_%t", id, pen, withStandard), func(t *testing.T) {
					fields := []netflow.DataField{{PenProvided: true, Pen: pen, Type: id, Value: []byte{0, 0, 3, 232}}}
					if withStandard {
						fields = append(fields, netflow.DataField{Type: id, Value: []byte{0, 0, 7, 208}})
					}
					sets := []netflow.OptionsDataFlowSet{{Records: []netflow.OptionsDataRecord{{OptionsValues: fields}}}}
					rate, found, err := SearchNetFlowOptionDataSets(sets)
					require.NoError(t, err)
					require.Equal(t, withStandard, found)
					if withStandard {
						require.Equal(t, uint32(2000), rate)
					} else {
						require.Zero(t, rate)
					}
				})
			}
		}
	}
}

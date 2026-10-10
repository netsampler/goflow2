package netflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func formatTestPackets() map[string]fmt.Stringer {
	flowSets := []interface{}{
		TemplateFlowSet{
			FlowSetHeader: FlowSetHeader{Id: 256, Length: 12},
			Records:       []TemplateRecord{{TemplateId: 256, FieldCount: 1, Fields: []Field{{Type: 1, Length: 4}}}},
		},
		NFv9OptionsTemplateFlowSet{
			FlowSetHeader: FlowSetHeader{Id: 1, Length: 18},
			Records: []NFv9OptionsTemplateRecord{{
				TemplateId: 257, ScopeLength: 4, OptionLength: 4,
				Scopes: []Field{{Type: 1, Length: 4}}, Options: []Field{{Type: 2, Length: 4}},
			}},
		},
		IPFIXOptionsTemplateFlowSet{
			FlowSetHeader: FlowSetHeader{Id: 3, Length: 18},
			Records: []IPFIXOptionsTemplateRecord{{
				TemplateId: 258, FieldCount: 2, ScopeFieldCount: 1,
				Scopes: []Field{{Type: 1, Length: 4}}, Options: []Field{{Type: 2, Length: 4}},
			}},
		},
		DataFlowSet{
			FlowSetHeader: FlowSetHeader{Id: 256, Length: 8},
			Records:       []DataRecord{{Values: []DataField{{Type: 1, Value: []byte{0, 0, 0, 10}}}}},
		},
		RawFlowSet{FlowSetHeader: FlowSetHeader{Id: 300, Length: 6}, Records: []byte{1, 2}},
		OptionsDataFlowSet{
			FlowSetHeader: FlowSetHeader{Id: 257, Length: 12},
			Records: []OptionsDataRecord{{
				ScopesValues:  []DataField{{Type: 1, Value: []byte{0, 0, 0, 1}}},
				OptionsValues: []DataField{{Type: 2, Value: []byte{0, 0, 0, 2}}},
			}},
		},
		"unsupported", nil,
	}
	return map[string]fmt.Stringer{
		"netflow-v9": NFv9Packet{Version: 9, FlowSets: flowSets},
		"ipfix":      IPFIXPacket{Version: 10, FlowSets: flowSets},
	}
}

func TestPacketFlowSetFormatting(t *testing.T) {
	for name, packet := range formatTestPackets() {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", name+"-flowsets.txt"))
			require.NoError(t, err)
			text := packet.String()
			start := strings.Index(text, "  FlowSets (")
			require.NotEqual(t, -1, start)
			require.Equal(t, string(want), text[start:])
		})
	}
}

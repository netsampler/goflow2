package netflow

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFlowSetHeaderSize guards the constant used by the decoder against changes
// to the FlowSetHeader struct.
func TestFlowSetHeaderSize(t *testing.T) {
	assert.Equal(t, binary.Size(FlowSetHeader{}), flowSetHeaderSize)
}

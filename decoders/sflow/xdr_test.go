package sflow

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXDRReaderBasics(t *testing.T) {
	buf := bytes.NewBuffer([]byte{
		0, 0, 0, 5, // u32
		0, 0, 0, 0, 0, 0, 1, 0, // u64
		1, 2, 3, 4, 5, 6, // 6 bytes
		0, 0, 0, 1, 0, 0, 0, 2, // u32s
		0xaa, 0xbb, // trailing, left unread
	})
	r := newXDRReader(buf)

	assert.Equal(t, uint32(5), r.u32())
	assert.Equal(t, uint64(256), r.u64())
	assert.Equal(t, []byte{1, 2, 3, 4, 5, 6}, r.bytes(6))
	assert.Equal(t, []uint32{1, 2}, r.u32s(2))
	require.NoError(t, r.err)
	assert.Equal(t, 2, r.remaining())
	assert.Equal(t, []byte{0xaa, 0xbb}, r.rest())

	// commit advances the underlying buffer by exactly what was consumed.
	r.commit(buf)
	assert.Equal(t, []byte{0xaa, 0xbb}, buf.Bytes())
}

func TestXDRReaderStickyError(t *testing.T) {
	r := newXDRReader(bytes.NewBuffer([]byte{0, 0, 0, 7, 0, 1}))

	assert.Equal(t, uint32(7), r.u32())
	assert.Equal(t, uint32(0), r.u32(), "short read yields zero")
	assert.True(t, errors.Is(r.err, io.ErrUnexpectedEOF))

	// Once failed, every read returns the zero value and the first error sticks.
	assert.Nil(t, r.bytes(1))
	assert.Equal(t, uint64(0), r.u64())
	assert.Nil(t, r.u32s(1))
	assert.True(t, errors.Is(r.err, io.ErrUnexpectedEOF))
}

func TestXDRReaderOpaqueAndString(t *testing.T) {
	r := newXDRReader(bytes.NewBuffer([]byte{
		0, 0, 0, 3, 'a', 'b', 'c', 0, // variable opaque, 3 bytes + 1 pad
		0, 0, 0, 4, 'w', 'x', 'y', 'z', // variable opaque, 4 bytes, no pad
		1, 2, 3, 4, 5, 0, 0, 0, // fixed opaque of 5 + 3 pad
		0, 0, 0, 9, // marker after padding
	}))

	assert.Equal(t, "abc", r.str())
	assert.Equal(t, []byte("wxyz"), r.opaqueVar())
	assert.Equal(t, []byte{1, 2, 3, 4, 5}, r.opaque(5))
	assert.Equal(t, uint32(9), r.u32())
	require.NoError(t, r.err)

	// Missing padding is an error, not silently accepted.
	r = newXDRReader(bytes.NewBuffer([]byte{0, 0, 0, 3, 'a', 'b', 'c'}))
	assert.Nil(t, r.opaqueVar())
	assert.True(t, errors.Is(r.err, io.ErrUnexpectedEOF))
}

func TestXDRReaderSubDoesNotOverrun(t *testing.T) {
	r := newXDRReader(bytes.NewBuffer([]byte{0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3}))

	sub := r.sub(8)
	require.NoError(t, r.err)
	assert.Equal(t, uint32(1), sub.u32())
	assert.Equal(t, uint32(2), sub.u32())
	assert.Equal(t, uint32(0), sub.u32(), "sub reader must not see the parent's remaining bytes")
	assert.True(t, errors.Is(sub.err, io.ErrUnexpectedEOF))

	// The parent skipped the sub range and is unaffected by the sub error.
	require.NoError(t, r.err)
	assert.Equal(t, uint32(3), r.u32())

	// Sub-slices have clipped capacity: appending to a value cannot leak into
	// the following bytes of the datagram.
	r = newXDRReader(bytes.NewBuffer([]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	b := r.bytes(4)
	assert.Equal(t, 4, cap(b))
}

func TestDecodedAddressesAliasPayload(t *testing.T) {
	packet := Packet{
		Version:      5,
		IPVersion:    1,
		AgentIP:      []byte{10, 0, 0, 1},
		SamplesCount: 1,
		Samples: []interface{}{
			FlowSample{
				Header:           SampleHeader{Format: SAMPLE_FORMAT_FLOW, SampleSequenceNumber: 1},
				FlowRecordsCount: 2,
				Records: []FlowRecord{
					{Data: SampledIPv4{SampledIPBase: SampledIPBase{SrcIP: []byte{192, 0, 2, 1}, DstIP: []byte{192, 0, 2, 2}}}},
					{Data: ExtendedRouter{NextHop: []byte{203, 0, 113, 9}}},
				},
			},
		},
	}
	encoded, err := EncodeMessage(&packet)
	require.NoError(t, err)

	var decoded Packet
	require.NoError(t, DecodeMessageVersion(bytes.NewBuffer(encoded), &decoded))
	sample := decoded.Samples[0].(FlowSample)
	ip := sample.Records[0].Data.(SampledIPv4)
	router := sample.Records[1].Data.(ExtendedRouter)

	assert.Equal(t, []byte{10, 0, 0, 1}, []byte(decoded.AgentIP))
	assert.Equal(t, []byte{192, 0, 2, 1}, []byte(ip.SrcIP))
	assert.Equal(t, []byte{203, 0, 113, 9}, []byte(router.NextHop))

	// Byte fields are views over the datagram, like HeaderData always was:
	// changing the datagram changes the decoded value.
	for i := range encoded {
		encoded[i] = 0xff
	}
	assert.Equal(t, []byte{0xff, 0xff, 0xff, 0xff}, []byte(ip.SrcIP))
	assert.Equal(t, []byte{0xff, 0xff, 0xff, 0xff}, []byte(decoded.AgentIP))
}

func TestDecodeIPAdvancesBuffer(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0, 0, 0, 1, 10, 0, 0, 1, 0, 0, 0, 42})
	version, ip, err := DecodeIP(buf)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), version)
	assert.Equal(t, []byte{10, 0, 0, 1}, ip)
	assert.Equal(t, []byte{0, 0, 0, 42}, buf.Bytes(), "buffer position must move past the address")

	_, _, err = DecodeIP(bytes.NewBuffer([]byte{0, 0, 0, 2, 1, 2, 3}))
	assert.Error(t, err)

	version, ip, err = DecodeIP(bytes.NewBuffer([]byte{0, 0, 0, 0}))
	require.NoError(t, err)
	assert.Equal(t, uint32(0), version)
	assert.Nil(t, ip)
}

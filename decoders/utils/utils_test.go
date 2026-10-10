package utils

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testBinaryRead(buf BytesBuffer, data any) error {
	order := binary.BigEndian
	return BinaryRead(buf, order, data)
}

func testBinaryReadComparison(buf BytesBuffer, data any) error {
	order := binary.BigEndian
	return binary.Read(buf, order, data)
}

type benchFct func(buf BytesBuffer, data any) error

func TestBinaryRead(t *testing.T) {
	wantInteger := uint32(0x01020304)
	for _, tc := range []struct {
		name string
		data []byte
		dest any
		want any
	}{
		{"integer", []byte{1, 2, 3, 4}, new(uint32), &wantInteger},
		{"bytes", []byte{1, 2, 3, 4}, make([]byte, 4), []byte{1, 2, 3, 4}},
		{"uints", []byte{1, 2, 3, 4, 5, 6, 7, 8}, make([]uint32, 2), []uint32{0x01020304, 0x05060708}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := bytes.NewBuffer(tc.data)
			require.NoError(t, BinaryRead(buf, binary.BigEndian, tc.dest))
			assert.Equal(t, tc.want, tc.dest)
			assert.Empty(t, buf.Bytes())
		})
	}
}

func TestBinaryDecoderConsumesValues(t *testing.T) {
	buf := bytes.NewBuffer([]byte{1, 2, 3, 4, 5, 6})
	var first uint16
	var second uint32
	require.NoError(t, BinaryDecoder(buf, &first, &second))
	assert.Equal(t, uint16(0x0102), first)
	assert.Equal(t, uint32(0x03040506), second)
	assert.Empty(t, buf.Bytes())
	assert.ErrorIs(t, BinaryDecoder(buf, &first), io.ErrUnexpectedEOF)
}

type testBuf struct {
	buf []byte
	off int
}

func newTestBuf(data []byte) *testBuf {
	return &testBuf{
		buf: data,
	}
}

func (b *testBuf) Next(n int) []byte {
	if n > len(b.buf)-b.off {
		n = len(b.buf) - b.off
	}
	data := b.buf[b.off : b.off+n]
	b.off += n
	return data
}

func (b *testBuf) Reset() {
	b.off = 0
}

func (b *testBuf) Read(p []byte) (int, error) {
	if len(b.buf) == 0 || b.off >= len(b.buf) {
		return 0, io.EOF
	}

	n := copy(p, b.buf[b.off:])
	b.off += n
	return n, nil
}

func benchBinaryRead(b *testing.B, buf *testBuf, dest any, cmp bool) {
	var fct benchFct
	if cmp {
		fct = testBinaryReadComparison
	} else {
		fct = testBinaryRead
	}
	for n := 0; n < b.N; n++ {
		if err := fct(buf, dest); err != nil {
			b.Fatal(err)
		}
		buf.Reset()
	}
}

func BenchmarkBinaryReadIntegerBase(b *testing.B) {
	buf := newTestBuf([]byte{1, 2, 3, 4})
	var dest uint32
	benchBinaryRead(b, buf, &dest, false)
}

func BenchmarkBinaryReadIntegerComparison(b *testing.B) {
	buf := newTestBuf([]byte{1, 2, 3, 4})
	var dest uint32
	benchBinaryRead(b, buf, &dest, true)
}

func BenchmarkBinaryReadByteBase(b *testing.B) {
	buf := newTestBuf([]byte{1, 2, 3, 4})
	var dest byte
	benchBinaryRead(b, buf, &dest, false)
}

func BenchmarkBinaryReadByteComparison(b *testing.B) {
	buf := newTestBuf([]byte{1, 2, 3, 4})
	var dest byte
	benchBinaryRead(b, buf, &dest, true)
}

func BenchmarkBinaryReadBytesBase(b *testing.B) {
	buf := newTestBuf([]byte{1, 2, 3, 4})
	dest := make([]byte, 4)
	benchBinaryRead(b, buf, dest, false)
}

func BenchmarkBinaryReadBytesComparison(b *testing.B) {
	buf := newTestBuf([]byte{1, 2, 3, 4})
	dest := make([]byte, 4)
	benchBinaryRead(b, buf, dest, true)
}

func BenchmarkBinaryReadUintsBase(b *testing.B) {
	buf := newTestBuf([]byte{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4})
	dest := make([]uint32, 4)
	benchBinaryRead(b, buf, dest, false)
}

func BenchmarkBinaryReadUintsComparison(b *testing.B) {
	buf := newTestBuf([]byte{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4})
	dest := make([]uint32, 4)
	benchBinaryRead(b, buf, dest, true)
}

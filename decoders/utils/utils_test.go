package utils

import (
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

func TestBinaryReadInteger(t *testing.T) {
	buf := newTestBuf([]byte{1, 2, 3, 4})
	var dest uint32
	err := testBinaryRead(buf, &dest)
	require.NoError(t, err)
	assert.Equal(t, uint32(0x1020304), dest)
}

func TestBinaryReadBytes(t *testing.T) {
	buf := newTestBuf([]byte{1, 2, 3, 4})
	dest := make([]byte, 4)
	err := testBinaryRead(buf, dest)
	require.NoError(t, err)
}

func TestBinaryReadUints(t *testing.T) {
	buf := newTestBuf([]byte{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4})
	dest := make([]uint32, 4)
	err := testBinaryRead(buf, dest)
	require.NoError(t, err)
	assert.Equal(t, uint32(0x1020304), dest[0])
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
	if n > len(b.buf) {
		return b.buf
	}
	return b.buf[0:n]
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
		fct(buf, dest)
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

func BBenchmarkBinaryReadByteComparison(b *testing.B) {
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

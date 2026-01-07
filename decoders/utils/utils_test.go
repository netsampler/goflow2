package utils

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testBinaryRead(buf *bytes.Buffer, data any) error {
	order := binary.BigEndian
	return BinaryRead(buf, order, data)
}

func testBinaryReadComparison(buf *bytes.Buffer, data any) error {
	order := binary.BigEndian
	return binary.Read(buf, order, data)
}

type benchFct func(buf *bytes.Buffer, data any) error

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

func newTestBuf(data []byte) *bytes.Buffer {
	buf := bytes.NewBuffer(make([]byte, 0, len(data)))
	buf.Write(data)
	return buf
}

func resetTestBuf(buf *bytes.Buffer, data []byte) {
	buf.Reset()
	buf.Write(data)
}

func benchBinaryRead(b *testing.B, buf *bytes.Buffer, data []byte, dest any, cmp bool) {
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
		resetTestBuf(buf, data)
	}
}

func BenchmarkBinaryReadIntegerBase(b *testing.B) {
	data := []byte{1, 2, 3, 4}
	buf := newTestBuf(data)
	var dest uint32
	benchBinaryRead(b, buf, data, &dest, false)
}

func BenchmarkBinaryReadIntegerComparison(b *testing.B) {
	data := []byte{1, 2, 3, 4}
	buf := newTestBuf(data)
	var dest uint32
	benchBinaryRead(b, buf, data, &dest, true)
}

func BenchmarkBinaryReadByteBase(b *testing.B) {
	data := []byte{1, 2, 3, 4}
	buf := newTestBuf(data)
	var dest byte
	benchBinaryRead(b, buf, data, &dest, false)
}

func BenchmarkBinaryReadByteComparison(b *testing.B) {
	data := []byte{1, 2, 3, 4}
	buf := newTestBuf(data)
	var dest byte
	benchBinaryRead(b, buf, data, &dest, true)
}

func BenchmarkBinaryReadBytesBase(b *testing.B) {
	data := []byte{1, 2, 3, 4}
	buf := newTestBuf(data)
	dest := make([]byte, 4)
	benchBinaryRead(b, buf, data, dest, false)
}

func BenchmarkBinaryReadBytesComparison(b *testing.B) {
	data := []byte{1, 2, 3, 4}
	buf := newTestBuf(data)
	dest := make([]byte, 4)
	benchBinaryRead(b, buf, data, dest, true)
}

func BenchmarkBinaryReadUintsBase(b *testing.B) {
	data := []byte{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	buf := newTestBuf(data)
	dest := make([]uint32, 4)
	benchBinaryRead(b, buf, data, dest, false)
}

func BenchmarkBinaryReadUintsComparison(b *testing.B) {
	data := []byte{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	buf := newTestBuf(data)
	dest := make([]uint32, 4)
	benchBinaryRead(b, buf, data, dest, true)
}

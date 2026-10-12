package sflow

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/netsampler/goflow2/v3/decoders/utils"
)

// xdrReader is a cursor over the bytes of a datagram that reads big-endian
// XDR values with a sticky error, so a run of reads needs one error check.
//
// It deliberately holds a byte slice and not a *bytes.Buffer: escape analysis
// is field-insensitive, and passing r.err to fmt.Errorf would otherwise make
// the compiler treat every field of r, including a buffer pointer, as escaping
// to the heap. With a slice, the nested per-sample and per-record readers stay
// on the stack.
type xdrReader struct {
	data []byte
	off  int
	err  error
}

func newXDRReader(buf *bytes.Buffer) xdrReader {
	return xdrReader{data: buf.Bytes()}
}

// commit advances buf past the bytes the reader consumed.
func (r *xdrReader) commit(buf *bytes.Buffer) {
	buf.Next(r.off)
}

func (r *xdrReader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// remaining returns the number of unread bytes.
func (r *xdrReader) remaining() int {
	return len(r.data) - r.off
}

// rest returns the unread bytes without consuming them.
func (r *xdrReader) rest() []byte {
	return r.data[r.off:]
}

// bytes returns the next n bytes as a sub-slice of the payload, without copying.
func (r *xdrReader) bytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.remaining() < n {
		r.fail(io.ErrUnexpectedEOF)
		return nil
	}
	b := r.data[r.off : r.off+n : r.off+n]
	r.off += n
	return b
}

// sub returns a reader over the next n bytes and consumes them.
func (r *xdrReader) sub(n int) xdrReader {
	return xdrReader{data: r.bytes(n), err: r.err}
}

func (r *xdrReader) skip(n int) {
	r.bytes(n)
}

func (r *xdrReader) u32() uint32 {
	b := r.bytes(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func (r *xdrReader) u64() uint64 {
	b := r.bytes(8)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

// u32s reads n big-endian uint32 values into a new slice.
func (r *xdrReader) u32s(n int) []uint32 {
	b := r.bytes(4 * n)
	if b == nil {
		return nil
	}
	out := make([]uint32, n)
	for i := range out {
		out[i] = binary.BigEndian.Uint32(b[4*i:])
	}
	return out
}

// opaque reads a fixed-length XDR opaque and its padding to a 4-byte boundary.
func (r *xdrReader) opaque(length uint32) []byte {
	data := r.bytes(int(length))
	if r.err != nil {
		return nil
	}
	if padding := (4 - (length % 4)) % 4; padding != 0 {
		r.skip(int(padding))
		if r.err != nil {
			return nil
		}
	}
	return data
}

// opaqueVar reads a variable-length XDR opaque: a length followed by the data.
func (r *xdrReader) opaqueVar() []byte {
	return r.opaque(r.u32())
}

func (r *xdrReader) str() string {
	return string(r.opaqueVar())
}

func writeXDROpaque(payload *bytes.Buffer, data []byte) error {
	if err := utils.WriteU32(payload, uint32(len(data))); err != nil {
		return err
	}
	if _, err := payload.Write(data); err != nil {
		return err
	}
	return writeXDRPadding(payload, uint32(len(data)))
}

func writeXDRString(payload *bytes.Buffer, value string) error {
	return writeXDROpaque(payload, []byte(value))
}

func writeXDRPadding(payload *bytes.Buffer, length uint32) error {
	padding := (4 - (length % 4)) % 4
	if padding == 0 {
		return nil
	}
	_, err := payload.Write(make([]byte, padding))
	return err
}

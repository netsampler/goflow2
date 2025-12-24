package utils

import (
	"bytes"
	"encoding/binary"
)

func WriteU8(buf *bytes.Buffer, v uint8) error {
	return buf.WriteByte(v)
}

func WriteU16(buf *bytes.Buffer, v uint16) error {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	_, err := buf.Write(b[:])
	return err
}

func WriteU32(buf *bytes.Buffer, v uint32) error {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	_, err := buf.Write(b[:])
	return err
}

func WriteU64(buf *bytes.Buffer, v uint64) error {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	_, err := buf.Write(b[:])
	return err
}

func WriteString(buf *bytes.Buffer, s string) error {
	if err := WriteU32(buf, uint32(len(s))); err != nil {
		return err
	}
	_, err := buf.WriteString(s)
	return err
}

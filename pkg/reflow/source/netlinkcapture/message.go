// Package netlinkcapture contains the framing shared by Linux packet exports.
package netlinkcapture

import (
	"encoding/binary"
	"fmt"
)

type Message struct {
	Type     uint16
	Flags    uint16
	Sequence uint32
	Data     []byte
}

func align(n int) int { return (n + 3) &^ 3 }

// Messages splits one datagram, preserving the kernel's native byte order.
func Messages(data []byte) ([]Message, error) {
	var messages []Message
	for len(data) >= 16 {
		n := int(binary.NativeEndian.Uint32(data))
		if n < 16 || n > len(data) {
			return nil, fmt.Errorf("invalid netlink message length %d", n)
		}
		messages = append(messages, Message{Type: binary.NativeEndian.Uint16(data[4:]), Flags: binary.NativeEndian.Uint16(data[6:]), Sequence: binary.NativeEndian.Uint32(data[8:]), Data: data[16:n]})
		step := align(n)
		if step > len(data) {
			break
		}
		data = data[step:]
	}
	return messages, nil
}

func EncodeMessage(typ, flags uint16, sequence uint32, payload []byte) []byte {
	data := make([]byte, 16+align(len(payload)))
	binary.NativeEndian.PutUint32(data, uint32(16+len(payload)))
	binary.NativeEndian.PutUint16(data[4:], typ)
	binary.NativeEndian.PutUint16(data[6:], flags)
	binary.NativeEndian.PutUint32(data[8:], sequence)
	copy(data[16:], payload)
	return data
}

// Attributes removes NLA flag bits; unknown attributes remain available.
func Attributes(data []byte) (map[uint16][]byte, error) {
	attrs := make(map[uint16][]byte)
	for len(data) >= 4 {
		n := int(binary.NativeEndian.Uint16(data))
		if n < 4 || n > len(data) {
			return nil, fmt.Errorf("invalid netlink attribute length %d", n)
		}
		attrs[binary.NativeEndian.Uint16(data[2:])&0x3fff] = data[4:n]
		step := align(n)
		if step > len(data) {
			break
		}
		data = data[step:]
	}
	return attrs, nil
}

func Attribute(typ uint16, payload []byte) []byte {
	data := make([]byte, align(4+len(payload)))
	binary.NativeEndian.PutUint16(data, uint16(4+len(payload)))
	binary.NativeEndian.PutUint16(data[2:], typ)
	copy(data[4:], payload)
	return data
}

func Uint32(v uint32) []byte { b := make([]byte, 4); binary.NativeEndian.PutUint32(b, v); return b }

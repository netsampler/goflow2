package sflow

import (
	"bytes"
	"io"

	"github.com/netsampler/goflow2/v3/decoders/utils"
)

func readXDROpaque(payload *bytes.Buffer) ([]byte, error) {
	var length uint32
	if err := utils.BinaryDecoder(payload, &length); err != nil {
		return nil, err
	}
	return readXDROpaqueWithLength(payload, length)
}

func readXDROpaqueWithLength(payload *bytes.Buffer, length uint32) ([]byte, error) {
	if int(length) > payload.Len() {
		return nil, io.ErrUnexpectedEOF
	}
	data := payload.Next(int(length))
	padding := (4 - (length % 4)) % 4
	if padding != 0 {
		if payload.Len() < int(padding) {
			return nil, io.ErrUnexpectedEOF
		}
		payload.Next(int(padding))
	}
	return data, nil
}

func readXDRString(payload *bytes.Buffer) (string, error) {
	data, err := readXDROpaque(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
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

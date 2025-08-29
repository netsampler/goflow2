//go:build !pcap
// +build !pcap

package utils

import (
	"errors"
)

type PcapReceiver struct{}

var _ Receiver = (*PcapReceiver)(nil)

var (
	ErrNotCompiled = errors.New("Not compiled in")
)

func NewPcapReceiver(cfg *UDPReceiverConfig) (*PcapReceiver, error) {
	return nil, ErrNotCompiled
}

func (r *PcapReceiver) Start(filename string, decodeFunc DecoderFunc) error {
	return ErrNotCompiled
}

func (r *PcapReceiver) Stop() error {
	return ErrNotCompiled
}

func (r *PcapReceiver) Errors() <-chan error {
	return nil
}

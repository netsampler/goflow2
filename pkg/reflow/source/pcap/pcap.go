//go:build reflow_pcap && cgo && !reflow_dpdk

// Package pcap implements live capture using the target platform's libpcap ABI.
package pcap

import (
	"fmt"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

type Source = capture.Source

func New(cfg config.SourceConfig) (*Source, error) {
	if cfg.Network != "pcap_live" {
		return nil, fmt.Errorf("unsupported source.network %q", cfg.Network)
	}
	if err := config.NormalizeCapture(&cfg); err != nil {
		return nil, err
	}
	return capture.New(cfg, &driver{cfg: cfg})
}

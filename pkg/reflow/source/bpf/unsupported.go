//go:build (!darwin && !freebsd && !openbsd && !netbsd) || reflow_pcap || reflow_dpdk || reflow_pfring

package bpf

import (
	"fmt"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

func New(config.SourceConfig) (*capture.Source, error) {
	return nil, fmt.Errorf("bpf requires a BSD/macOS platform profile")
}

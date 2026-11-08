//go:build !linux || reflow_pcap || reflow_dpdk

package psample

import (
	"fmt"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

func New(config.SourceConfig) (*capture.Source, error) {
	return nil, fmt.Errorf("psample requires the Linux platform profile")
}

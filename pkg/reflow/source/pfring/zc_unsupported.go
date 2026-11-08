//go:build linux && cgo && reflow_pfring && !reflow_pcap && !reflow_dpdk && (!reflow_pfring_zc || (!amd64 && !arm64))

package pfring

import (
	"fmt"
	"runtime"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

func newZC(config.SourceConfig) (*capture.Source, error) {
	return nil, fmt.Errorf("PF_RING ZC is not compiled for %s; rebuild with reflow_pfring_zc and a matching ZC SDK on amd64/arm64; standard pfring capture is available", runtime.GOARCH)
}

func ZCAvailable() bool { return false }

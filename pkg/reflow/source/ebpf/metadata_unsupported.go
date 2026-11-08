//go:build !linux || reflow_pcap || reflow_dpdk || reflow_pfring

package ebpf

import (
	"net"
	"regexp"
	"sync"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

type Source struct {
	shared                *capture.Source
	cfg                   config.SourceConfig
	agentIP               string
	captureInterfaceIndex int
	captureAny            bool
	interfaceFilter       *regexp.Regexp
	interfaceNames        map[uint32]string
	initializedInterfaces map[uint32]struct{}
	mu                    sync.Mutex
}

func firstInterfaceIP(*net.Interface) string {
	return "127.0.0.1"
}

func (*Source) refreshSampling([]*event.Event) {}

//go:build !linux || reflow_noebpf

package ebpf

import (
	"net"
	"regexp"
	"sync"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
)

type Source struct {
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

func firstSystemIP() string {
	return "127.0.0.1"
}

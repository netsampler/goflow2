//go:build !linux || reflow_noebpf

package ebpf

import "github.com/netsampler/goflow2/v3/pkg/reflow/config"

type Source struct {
	cfg                   config.SourceConfig
	agentIP               string
	captureInterfaceIndex int
}

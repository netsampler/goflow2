//go:build !linux || reflow_noebpf

package ebpf

import (
	"context"
	"fmt"
	"runtime"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

type perfEventReader struct{}

func New(cfg config.SourceConfig) (*Source, error) {
	if runtime.GOOS == "linux" {
		return nil, fmt.Errorf("source.network=%s is disabled in this build; rebuild without -tags reflow_noebpf", cfg.Network)
	}
	return nil, fmt.Errorf("source.network=%s requires linux, got %s", cfg.Network, runtime.GOOS)
}

func (s *Source) Start(context.Context, func(*event.Event) error) error {
	if runtime.GOOS == "linux" {
		return fmt.Errorf("source.network=ebpf is disabled in this build; rebuild without -tags reflow_noebpf")
	}
	return fmt.Errorf("source.network=ebpf requires linux, got %s", runtime.GOOS)
}

func (s *Source) Close() error {
	return nil
}

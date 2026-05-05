//go:build !linux

package ebpf

import (
	"context"
	"fmt"
	"runtime"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func New(cfg config.SourceConfig) (*Source, error) {
	return nil, fmt.Errorf("source.network=%s requires linux, got %s", cfg.Network, runtime.GOOS)
}

func (s *Source) Start(context.Context, func(*event.Event) error) error {
	return fmt.Errorf("source.network=ebpf requires linux, got %s", runtime.GOOS)
}

func (s *Source) Close() error {
	return nil
}

//go:build reflow_nopcap || !cgo

package pcap

import (
	"context"
	"fmt"
	"runtime"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

type Source struct{}

func New(cfg config.SourceConfig) (*Source, error) {
	if cfg.Network != "pcap_live" {
		return nil, fmt.Errorf("unsupported source.network %q", cfg.Network)
	}
	return nil, fmt.Errorf("source.network=pcap_live is disabled in this build; rebuild with CGO_ENABLED=1 and libpcap installed on %s", runtime.GOOS)
}

func (s *Source) InitEvents() ([]*event.Event, error) { return nil, nil }

func (s *Source) Start(context.Context, func(*event.Event) error) error {
	return fmt.Errorf("source.network=pcap_live is disabled in this build")
}

func (s *Source) Close() error { return nil }

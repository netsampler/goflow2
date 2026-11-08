//go:build !linux || reflow_pcap || reflow_dpdk

package conntrack

import (
	"context"
	"fmt"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

type Source struct{}

func New(config.SourceConfig) (*Source, error) {
	return nil, fmt.Errorf("conntrack input requires the Linux native platform profile")
}
func (*Source) Name() string                        { return "" }
func (*Source) Cache() *Cache                       { return nil }
func (*Source) Zone() uint16                        { return 0 }
func (*Source) InitEvents() ([]*event.Event, error) { return nil, nil }
func (*Source) Start(context.Context, func(*event.Event) error) error {
	return fmt.Errorf("conntrack input is unavailable")
}
func (*Source) Close() error { return nil }

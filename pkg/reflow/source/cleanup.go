package source

import "github.com/netsampler/goflow2/v3/pkg/reflow/source/dpdk"

// Cleanup releases process-wide SDK resources after all source goroutines stop.
func Cleanup() error { return dpdk.Cleanup() }

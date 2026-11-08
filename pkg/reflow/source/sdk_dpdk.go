//go:build reflow_dpdk

package source

import "github.com/netsampler/goflow2/v3/pkg/reflow/source/dpdk"

func SDKVersions() string { return "sdk=" + dpdk.SDKVersion() }

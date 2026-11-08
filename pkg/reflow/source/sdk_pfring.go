//go:build reflow_pfring

package source

import "github.com/netsampler/goflow2/v3/pkg/reflow/source/pfring"

func SDKVersions() string { return "sdk=" + pfring.SDKVersion() }

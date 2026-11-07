//go:build reflow_pcap

package source

import "github.com/netsampler/goflow2/v3/pkg/reflow/source/pcap"

func SDKVersions() string { return "sdk=" + pcap.SDKVersion() }

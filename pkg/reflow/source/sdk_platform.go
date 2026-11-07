//go:build !reflow_pcap && !reflow_dpdk && !reflow_pfring

package source

func SDKVersions() string { return "sdk=Go-native" }

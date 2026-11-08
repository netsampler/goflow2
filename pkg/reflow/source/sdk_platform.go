//go:build !reflow_pcap && !reflow_dpdk

package source

func SDKVersions() string { return "sdk=Go-native" }

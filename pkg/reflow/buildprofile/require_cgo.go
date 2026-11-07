//go:build !cgo && (reflow_pcap || reflow_dpdk || reflow_pfring)

package buildprofile

var _ = reflow_library_profiles_require_CGO_ENABLED_1

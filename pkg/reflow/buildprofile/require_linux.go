//go:build !linux && (reflow_dpdk || reflow_pfring)

package buildprofile

var _ = reflow_accelerated_profiles_require_linux

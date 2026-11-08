//go:build reflow_pcap && reflow_dpdk

package buildprofile

// Deliberately fail builds that combine mutually exclusive SDK profiles.
var _ = reflow_capture_profiles_are_mutually_exclusive

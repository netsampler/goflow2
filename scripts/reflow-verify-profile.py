#!/usr/bin/env python3
"""Check a runnable artifact's reported capture manifest against its profile."""
import subprocess
import sys

binary, goos, arch, profile = sys.argv[1:]
expected = {"pcap": ["pcap_live"], "dpdk": ["dpdk"]}.get(profile)
if expected is None:
    expected = {"linux": ["af_packet", "ebpf", "xdp", "nflog", "psample"],
                "darwin": ["bpf", "pktap"], "windows": ["pktmon"],
                "freebsd": ["bpf"], "openbsd": ["bpf"], "netbsd": ["bpf"]}[goos]
output = subprocess.check_output([binary, "-version"], text=True)
assert f"profile={profile}" in output, output
assert "capture=[" + " ".join(expected) + "]" in output, output
print(output.strip())

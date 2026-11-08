# Reflow capture inputs

Reflow v3 separates live packet acquisition from decoding and processing. Select
an input explicitly; the binary never silently falls back to another capture
engine. Socket inputs, streams, and offline pcap/pcapng processing work in every
profile. ARMv7 is a supported architecture, alongside amd64 and arm64 on Linux.

## What should I use?

- For a first live capture, use **`pcap_live` with a binary from the `-pcap`
  release archive**. Reflow builds these binaries with libpcap on Linux, macOS,
  and BSD. On Windows, the `pcap` build uses [Npcap](https://npcap.com/).
- To send captured traffic to a flow collector, use
  [IPFIX over UDP with packet aggregation](#export-live-traffic-as-ipfix).
- To read a capture file, use `stream:traffic.pcap:pcap` or
  `stream:traffic.pcapng:pcapng` with the `platform` build.
- To collect flows from a router, use `udp::4739:flow` with the `platform` build.
  Replace `4739` with the exporter's destination port.

### Try live capture on loopback

Extract the `-pcap` archive for your OS and architecture. From the extracted
directory, run `./reflow -version` (or `.\reflow.exe -version` on Windows) and
look for `profile=pcap` and `capture=[pcap_live]`. If needed, add `sudo` on Unix
or open an administrator PowerShell on Windows to get capture permissions.

On Linux, use `lo`. On macOS and BSD, replace it with `lo0`.

```sh
./reflow -input pcap_live:lo:bytes -output json:stdout
```

On Windows with Npcap installed, use the [Npcap loopback device](https://npcap.com/guide/npcap-devguide.html#for-software-that-uses-npcap-loopback-feature).

```powershell
.\reflow.exe -input 'pcap_live:\Device\NPF_Loopback:bytes' -output json:stdout
```

In a second terminal, run `ping 127.0.0.1`. You should see packet events as JSON
in the first terminal. Press Ctrl+C to stop. To capture regular network
traffic, replace the loopback device with your network adapter.

### Capture from Docker

On a Linux amd64 Docker host, use the `pcap` image to capture a host network
interface. The image packages the Linux amd64 `pcap` release binary and its
libpcap runtime. Append `-pcap` to a release tag to pin that release.

```sh
docker run --rm --network host --cap-drop ALL --cap-add NET_RAW \
  ghcr.io/netsampler/reflow:pcap \
  -input pcap_live:eth0:bytes -output json:stdout
```

Replace `eth0` with your capture interface. The same tags are available on
Docker Hub as `netsampler/reflow`.

### Export live traffic as IPFIX

Use `-agg` to combine captured packets into flows and send the resulting IPFIX
records to your collector over UDP.

```sh
./reflow -input pcap_live:eth0:bytes -agg -output ipfix:udp:192.0.2.10:4739
```

Replace `eth0` with your capture interface and `192.0.2.10:4739` with your
collector's IPFIX listener address and port.

## Release profiles and filenames

Choose a release by OS, architecture, and capture profile. Every profile includes
flow collection, JSON and offline pcap/pcapng streams, processing, aggregation,
and the same output encoders. The live capture features differ below; socket
support follows the operating system.

| OS | Architectures (`<arch>`) | Profile | Live capture features | Release archive | Runtime requirements |
| --- | --- | --- | --- | --- | --- |
| Linux | `amd64`, `arm64`, `armv7` | `platform` | `af_packet` mmap capture, `ebpf` SKB/conntrack metadata, passive `xdp`, firewall copies via `nflog`, ASIC samples via `psample` | `reflow-<version>-linux-<arch>-platform.tar.gz` | Applicable kernel capabilities and capture permissions; no capture SDK |
| Linux | `amd64`, `arm64`, `armv7` | `pcap` | `pcap_live` | `reflow-<version>-linux-<arch>-pcap.tar.gz` | libpcap and capture permissions |
| Linux | `amd64`, `arm64` | `dpdk` | `dpdk` RX queues | `reflow-<version>-linux-<arch>-dpdk.tar.gz` | DPDK runtime and configured ports/memory |
| macOS | `amd64`, `arm64` | `platform` | Native `bpf` and `pktap` metadata capture | `reflow-<version>-darwin-<arch>-platform.tar.gz` | BPF access; root privileges for pktap interface creation; no capture SDK |
| macOS | `amd64`, `arm64` | `pcap` | `pcap_live` | `reflow-<version>-darwin-<arch>-pcap.tar.gz` | System libpcap and capture permissions |
| Windows | `amd64`, `arm64` | `platform` | Native `pktmon` streaming | `reflow-<version>-windows-<arch>-platform.zip` | Modern Packet Monitor streaming API and administrator rights; no Npcap dependency |
| Windows | `amd64`, `arm64` | `pcap` | `pcap_live` through Npcap | `reflow-<version>-windows-<arch>-pcap.zip` | Separately installed Npcap runtime/driver and capture permissions |
| FreeBSD, OpenBSD, NetBSD | `amd64`, `arm64` | `platform` | Native `bpf` | `reflow-<version>-<bsd>-<arch>-platform.tar.gz` | BPF device access; no capture SDK |
| FreeBSD, OpenBSD, NetBSD | `amd64`, `arm64` | `pcap` | `pcap_live` | `reflow-<version>-<bsd>-<arch>-pcap.tar.gz` | libpcap and BPF device access |

`<version>` is the Makefile's `VERSION` with its leading `v` removed. The default
and current release workflows use `git describe --abbrev --long HEAD`, which
includes a commit count and hash even at a tag: for example,
`reflow-3.0.0-0-g1234567-linux-armv7-platform.tar.gz`. Setting `VERSION=v3.0.0`
explicitly produces `reflow-3.0.0-linux-armv7-platform.tar.gz`; the shorter
examples below use that override. `<bsd>` is one of `freebsd`, `openbsd`, or
`netbsd`. macOS uses the filename token `darwin`. ARMv7 uses `armv7` in filenames
and `GOARCH=arm GOARM=7` when building.

| Item | Naming convention |
| --- | --- |
| Unix release download | `reflow-<version>-<os>-<arch>-<profile>.tar.gz` |
| Windows release download | `reflow-<version>-windows-<arch>-<profile>.zip` |
| Download checksum | The complete archive filename followed by `.sha256`, such as `reflow-3.0.0-windows-arm64-pcap.zip.sha256` |
| Extracted Unix executable | `reflow-package-<os>-<arch>-<profile>/reflow` |
| Extracted Windows executable | `reflow-package-windows-<arch>-<profile>/reflow.exe` |
| Local `make build-reflow` output | `dist/reflow-<version>-<os>-<arch>-<profile>`; Windows adds `.exe` when built with `EXTENSION=.exe` |

The versioned raw executable is a local build output; release downloads contain
the archive and checksum. Platform builds require no cgo. The library profiles
directly link their selected SDK. Runtime libraries are not bundled in the
archives and must be installed even when running a socket or file input.

Inspect `-version` for the actual target, profile, compiled capture inputs, and
library version, and `-list-options` for input helpers.

```sh
GOOS=linux GOARCH=arm GOARM=7 make build-reflow REFLOW_PROFILE=platform
GOOS=darwin GOARCH=arm64 make build-reflow REFLOW_PROFILE=pcap
GOOS=linux GOARCH=amd64 make build-reflow REFLOW_PROFILE=dpdk
```

Use a C compiler and SDK for the **target** OS/architecture for library builds.
The Makefile sets cgo and the profile tags. The underlying tags are
`reflow_pcap` and `reflow_dpdk`. Select one tag per library build.
Without a profile tag, `go build ./cmd/reflow` builds the platform profile.
The pinned [DPDK SDK build recipe](../scripts/reflow-build-dpdk-sdk.sh) disables
DPDK's `net/pcap` driver to keep libpcap out of the DPDK profile. DPDK's ARMv7
build requires NEON.

Linux ARM64 CI builds and Docker packaging use native ARM64 runners. CI
cross-compiles Linux ARMv7 platform and pcap builds on amd64 and uses QEMU to
run the target tests on main and tags. CI caches the DPDK SDK by target
architecture, build-image digest, and a hash of the DPDK SDK build recipe.
Cache hits reuse the SDK while still running the Reflow tests, vet, packaging,
and dependency checks.
SDK versions or build-option changes invalidate the cache through the script hash.

## Configuration and migration

Common live capture settings belong in `capture`. Source identity and control
record refresh remain on the source itself:

```yaml
sources:
  - network: af_packet
    type: bytes
    source_id: 10
    source_init_refresh_ms: 30000
    capture:
      interface: eth0
      snaplen: 65535
      sample_every: 10
      sample_offset: 0
      direction: both
      promiscuous: true
      af_packet:
        block_size: 1048576
        num_blocks: 8
        block_timeout_ms: 100
```

Move `interface`, `snaplen`, `sample_every`, `sample_offset`, `interface_filter`,
and `ebpf` from the old source level into `capture`. Move `ebpf.direction` into
`capture.direction`. Legacy flat settings remain readable for existing callers;
generated YAML always uses the capture block.

`-input <backend>:<target>:bytes` remains supported, including common query
options `snaplen`, `sample_every`, `sample_offset`, `direction`, `promiscuous`,
`interface_filter`, and source-level `source_init_refresh_ms`. Use YAML for
backend-specific settings. For example:

```sh
reflow -input 'af_packet:eth0:bytes?sample_every=10' -output json:stdout
reflow -input 'xdp:eth0:bytes?direction=ingress' -output json:stdout
reflow -input pktap:all:bytes -output json:stdout
reflow -input nflog:42:bytes -output json:stdout
reflow -input psample:1,2:bytes -output json:stdout
reflow -input pcap_live:en0:bytes -output json:stdout
```

Defaults are snaplen 65535 and sample_every 1. Packet Monitor defaults to its
maximum 9000-byte capture length. Direction defaults to ingress for XDP and DPDK
and to both for other inputs. A driver rejects explicitly requested
capabilities it cannot implement. Capture engines run in the process's network
namespace; deploy or enter the intended namespace before starting Reflow.

Packet buffers become pipeline-owned before a driver reuses them. Original and
captured lengths remain separate. An engine's unavailable metadata is omitted;
capture delivery loss, upstream sampling, and locally skipped samples are not
interchangeable counters. Sampling settings describe deterministic selection in
Reflow's receive order, not ordering across independent NIC queues.

## Linux platform engines

For general-purpose Linux packet capture, prefer `ebpf` when your deployment
allows it. It offers the most capture features in the `platform` build,
including capture across interfaces with `interface: any`, SKB metadata, and
adaptive sampling without a socket handoff. It requires BPF program and map
permissions in addition to packet-socket access. The `perf` transport can
require further permissions. Use `af_packet` if eBPF loading is restricted or
you need promiscuous capture. See [eBPF transport requirements](#ebpf-packet-delivery-transport).

`af_packet` uses TPACKET_V3 mmap rings and reports capture timestamps, direction,
VLAN information, and available socket-drop counts. It requires packet-socket
privileges.

Linux `af_packet`, `ebpf`, and `xdp` support explicit kernel packet selection
and optional adaptive divisors. See [kernel sampling](reflow-kernel-sampling.md)
and [adaptive sampling](reflow-adaptive-sampling.md) for configuration and the
per-record sampling rate required by aggregation and export.

`sample_every: 1` requests every packet; it does not guarantee lossless capture.
When downstream processing cannot keep up, the capture ring can overflow even
while the interface's RX/TX drop counters stay at zero. Periodic source options
retain the observed cumulative capture-drop count and log a warning when it
increases. Those drops are delivery loss, not a sampling divisor: do not scale
the missing bytes as though they were intentionally sampled. Larger
`capture.af_packet.block_size` / `num_blocks` rings can absorb bursts, but do not
raise sustained processing capacity.

For CPU profiling on a capture host, run the same pipeline in the foreground:

```sh
reflow -config /etc/reflow/exporter.yaml -cpuprofile /tmp/reflow-cpu.pprof
```

Reproduce the busy interval, then stop with SIGINT or SIGTERM so the profile is
finalized. Copy the profile to a workstation and inspect it with the matching
executable, for example
`go run cmd/pprof -top ./reflow /tmp/reflow-cpu.pprof`.
Profiling is disabled unless a file is specified; it opens no network listener.

`ebpf` attaches a socket filter to an AF_PACKET socket and delivers packet records
through a perf buffer or a BPF ring buffer. Configure SKB metadata and the built-in
conntrack lookup under `capture.ebpf`.

`xdp` attaches a passive ingress observer, copies packets to a perf buffer, and
always returns XDP_PASS, including when capture delivery fails. It never replaces
an existing program. Select `capture.xdp.mode: driver` (default) or `generic`;
unsupported modes fail explicitly. Closing the source detaches its own link.
The effective capture snaplen can be bounded by perf-record limits and is
reported in `capture_backend_snaplen`.

### Firewall packet copies: NFLOG

Configure nftables or iptables to copy matching packets to the chosen NFLOG
group. Reflow binds that group without returning firewall verdicts:

```sh
# Assuming an existing inet filter table with a forward chain:
nft add rule inet filter forward log group 42 snaplen 65535 queue-threshold 1
# Equivalent iptables capture target:
iptables -A FORWARD -j NFLOG --nflog-group 42 --nflog-size 65535
reflow -input nflog:42:bytes -output json:stdout
```

Choose an unused group. Reflow does not create, flush, or remove firewall rules.
Packet copies include available address family, hook, mark, prefix, interfaces,
and timestamp metadata. Packet contents may begin at the IP layer depending on
the Netfilter family/hook. ULOG and inline NFQUEUE are not implemented.

### SONiC/SAI and psample

Use `psample` to receive existing generic-Netlink exports, including ASIC samples
exported through SONiC's SAI hostif/trap configuration. The generic-Netlink family
`psample` and multicast group `packets` are discovered at runtime. The target
selects numeric sample groups (`1,2`) or `all`; these numbers are distinct from
the dynamically discovered Netlink multicast group ID.

```yaml
sources:
  - network: psample
    type: bytes
    source_id: 20
    capture:
      psample:
        groups: [1, 2]
        port_map:
          "1": Ethernet0
          "2": Ethernet4
```

The platform must already export samples in the process's namespace. Configure
SONiC/SAI sampling, hostifs, trap channels, and control-plane protection through
the platform's existing management tools. Upstream sample rates, original size,
port identifiers, and available queue/timestamp attributes are retained. Explicit
`port_map` entries translate exported identifiers to local interfaces. Sequence
gaps are delivery diagnostics and do not prove ASIC forwarding loss.

If mirrored packets appear on a host netdev, use `af_packet` or `pcap_live`.
SAI defines netdev, FD, and generic-Netlink host interfaces, but ASIC delivery is
platform-dependent. Reflow does not open another process's SAI session or
configure mirror sessions, and psample is not a universal mirror-to-CPU API.
Vendor-only SDK callbacks require an adapter in the owning switch process.

## macOS, BSD, and Windows

`bpf` reads native BPF devices with per-OS record layouts and ioctls. It requires
access to `/dev/bpf*` (or `/dev/bpf` on systems exposing a cloning device).
Direction filtering follows each OS's BPF capabilities.

macOS `pktap` creates its own pktap interface, reads through BPF, and interprets
v1/v2 headers and the inner link type. It supports a selected interface or `all`,
including loopback/tunnel traffic when selected. It emits available process and
direction metadata and destroys only its own interface on normal cleanup.
PKTAP's private ABI is checked through captured-header fixtures; a macOS update
can require updating the adapter. It does not use libpcap or enable promiscuity.

Windows `pktmon` uses `Pktmonapi.dll` streaming and independent sessions,
correlates component IDs with network adapters, and copies callback data through
a bounded queue. Queue and stream losses are reported. A Windows build exposing
the modern Packet Monitor streaming API and administrator rights is required;
API presence is detected at startup. No Npcap dependency is needed in this profile.

Pktmon can optionally apply [fixed or adaptive callback sampling](reflow-pktmon-sampling.md)
before Reflow copies and queues observations. This uses `sampling.location: callback`;
it preserves the divisor per packet and does not reduce the native stream's
upstream observation rate.

The pcap profile uses libpcap on Unix and Npcap on Windows. It supports Windows
ARM64 using a portable C ABI wrapper. On macOS it links the system libpcap.

## DPDK capture

DPDK configuration is under `capture.dpdk`: `port` is a DPDK device name,
`queues` selects a contiguous RX queue set starting at zero, `eal_args` supplies process-wide EAL arguments, and
`descriptors`/`mbufs` tune buffers. Sources share one EAL initialization and must
agree on arguments; a port's configuration is owned by one source. Reflow copies
multisegment mbufs before recycling them and releases EAL after all sources stop.
Configure the driver binding, hugepages, and CPU allocation before startup.

On a Linux host with both DPDK ports and regular kernel-managed interfaces,
run two Reflow processes. Use the `dpdk` build for the DPDK ports and the
`platform` build with `ebpf` or `af_packet` for the kernel-managed interfaces.
For IPFIX export, assign a different `encoder.templated_flow.observation_domain_id`
to each process, for example 100 for DPDK and 200 for platform capture. This lets
a shared collector distinguish the two streams even when they use the same source
IP address.

### eBPF packet-delivery transport

`capture.ebpf.transport` selects `perf` (the default) or `ringbuf`. `perf` uses
per-CPU delivery buffers, and `ringbuf` uses a shared BPF ring. Reflow returns
setup errors for the selected transport. A full ring counts as capture delivery
loss.

The same options are available as `transport` and `ring_size` input URL parameters.

```yaml
capture:
  interface: eth2
  snaplen: 256
  sampling:
    location: kernel
    every: 64
  ebpf:
    transport: ringbuf
    ring_size: 4194304
    conntrack: false
```

Both eBPF transports require permission to open the packet socket, load the BPF
program, and create BPF maps. With `perf`, Reflow also opens a CPU-wide software
event on each online CPU using `perf_event_open()` to deliver packet records.
When `kernel.perf_event_paranoid` is 1 or higher, this system-wide access requires
`CAP_PERFMON` or `CAP_SYS_ADMIN`. A container's seccomp profile must also allow
`perf_event_open()`. See the [Linux perf security guide](https://docs.kernel.org/admin-guide/perf-security.html#unprivileged-users)
and [Docker seccomp documentation](https://docs.docker.com/engine/security/seccomp/).

The ring-buffer transport requires BPF ring-buffer maps and helpers and a
supported socket filter JIT. It bypasses `perf_event_open()` and works without
`CONFIG_BPF_EVENTS`. ARM32 uses compatible sampling arithmetic and counters and
the upstream 32-bit ring-reader fix from cilium/ebpf v0.20.0. Reflow attempts full
SKB metadata first and uses compatibility metadata when optional context fields
are unavailable.

`ring_size` is the shared ring capacity in bytes (default 4 MiB), a power of two
at least as large as the kernel page size. Ring capture limits the packet prefix
to 32712 bytes so its per-CPU scratch value fits older kernel allocation limits;
the original SKB wire length is retained for byte accounting. The actual limit is
reported as `capture_backend_snaplen`. Only the captured prefix is delivered.

Kernel sampling, including adaptive rate changes, happens before the packet copy.
Each record retains its selected divisor. Copy and ring-output failures feed the
same cumulative capture-loss accounting and adaptive controller as perf capture.
With userspace sampling, ring capture delivers every packet to the shared adapter
and the configured systematic divisor is applied there exactly once. Shared
conntrack enrichment continues to use the independent named conntrack input.

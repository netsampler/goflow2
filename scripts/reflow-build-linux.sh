#!/usr/bin/env bash
# Run in a target-architecture Debian Go image (native or QEMU). External SDKs
# are built in the same architecture/libc environment as the executable.
set -euo pipefail

profile=${REFLOW_PROFILE:-platform}
arch=${GOARCH:-$(go env GOARCH)}
case "$profile" in platform|pcap|dpdk|pfring) ;; *) echo "unknown Reflow profile: $profile" >&2; exit 1 ;; esac
case "$arch" in amd64|arm64) ;; arm) export GOARM=7 ;; *) echo "unsupported Linux architecture: $arch" >&2; exit 1 ;; esac
export GOOS=linux GOARCH="$arch" GOTOOLCHAIN=local
export DEBIAN_FRONTEND=noninteractive
sdk_prefix=${REFLOW_SDK_PREFIX:-/opt/reflow-sdk}
sdk_work=$(mktemp -d)
trap 'rm -rf "$sdk_work"' EXIT

apt-get update -qq
apt-get install -y --no-install-recommends python3 file binutils pkg-config ca-certificates curl xz-utils

case "$profile" in
  platform)
    export CGO_ENABLED=0
    ;;
  pcap)
    apt-get install -y --no-install-recommends libpcap-dev
    export CGO_ENABLED=1
    ;;
  dpdk)
    # The null PMD supplies a hardware-independent integration fixture. Pcap
    # remains disabled even when a build environment happens to provide it.
    apt-get install -y --no-install-recommends meson ninja-build python3-pyelftools libnuma-dev
    version=25.11.4
    mkdir -p "$sdk_work/dpdk"
    curl --fail --location --retry 3 "https://fast.dpdk.org/rel/dpdk-${version}.tar.xz" -o "$sdk_work/dpdk.tar.xz"
    tar -C "$sdk_work/dpdk" --strip-components=1 -xf "$sdk_work/dpdk.tar.xz"
    meson setup "$sdk_work/dpdk/build" "$sdk_work/dpdk" \
      --prefix="$sdk_prefix" --libdir=lib -Dplatform=generic \
      -Ddefault_library=shared -Dtests=false -Dexamples= -Ddisable_drivers=net/pcap
    ninja -C "$sdk_work/dpdk/build" -j "$(nproc)"
    ninja -C "$sdk_work/dpdk/build" install
    export CGO_ENABLED=1
    export PKG_CONFIG_PATH="$sdk_prefix/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
    # DPDK's arm64 pkg-config flags include this valid GCC option.
    export CGO_CFLAGS_ALLOW='-moutline-atomics'
    export REFLOW_TEST_DPDK=1
    ;;
  pfring)
    apt-get install -y --no-install-recommends flex bison libnuma-dev libnl-3-dev libnl-genl-3-dev
    version=9.4.0
    mkdir -p "$sdk_work/pfring"
    curl --fail --location --retry 3 "https://github.com/ntop/PF_RING/archive/refs/tags/${version}.tar.gz" -o "$sdk_work/pfring.tar.gz"
    tar -C "$sdk_work/pfring" --strip-components=1 -xf "$sdk_work/pfring.tar.gz"
    # The bundled n2disk timeline reader is unrelated to packet capture and
    # otherwise reintroduces libpcap symbols even with --disable-bpf.
    rm -f "$sdk_work/pfring/userland/lib/libs"/libnpcap_*
    # PF_RING 9.4.0's --disable-bpf still compiles its libpcap input module.
    # Remove that optional module from this SDK build as well as its filter API.
    python3 - "$sdk_work/pfring/userland/lib" <<'PY'
import pathlib
import sys
root = pathlib.Path(sys.argv[1])
source = root / "pfring.c"
text = source.read_text()
include = '#include "pfring_mod_pcap.h"'
entry = '''  {
    .name = "pcap",
    .open = pfring_mod_pcap_open,
    .findalldevs = NULL
  },'''
assert include in text and entry in text, "PF_RING no-pcap patch no longer matches"
source.write_text(text.replace(include, "").replace(entry, ""))
makefile = root / "Makefile.in"
text = makefile.read_text()
assert "pfring_mod_pcap.o" in text
makefile.write_text(text.replace("pfring_mod_pcap.o", ""))
PY
    if [[ "$arch" == amd64 ]]; then default_zc=1; else default_zc=0; fi
    export PF_RING_ZC=${PF_RING_ZC:-$default_zc}
    if [[ -n ${PF_RING_ZC_ARCHIVE:-} ]]; then
      case "$arch" in amd64) zc_arch=x86_64 ;; arm64) zc_arch=aarch64 ;; *) echo "ZC SDK unsupported on $arch" >&2; exit 1 ;; esac
      cp "$PF_RING_ZC_ARCHIVE" "$sdk_work/pfring/userland/lib/libs/libpfring_zc_${zc_arch}.a"
    fi
    (
      cd "$sdk_work/pfring/userland"
      # No libpcap filter dependency, AF_XDP library, or unrelated flow table.
      ./configure --prefix="$sdk_prefix" --disable-bpf --disable-xdp --disable-ft --disable-archopt
      make -C lib -j "$(nproc)"
      make -C lib install
    )
    mkdir -p "$sdk_prefix/include/linux"
    cp "$sdk_work/pfring/kernel/linux/pf_ring.h" "$sdk_prefix/include/linux/"
    if [[ "$PF_RING_ZC" == 1 ]]; then
      test -f "$sdk_prefix/include/pfring_zc.h"
      nm -D "$sdk_prefix/lib/libpfring.so" | grep ' pfring_zc_create_cluster$'
    fi
    export CGO_ENABLED=1
    export CGO_CFLAGS="-I$sdk_prefix/include ${CGO_CFLAGS:-}"
    export CGO_LDFLAGS="-L$sdk_prefix/lib ${CGO_LDFLAGS:-}"
    ;;
esac

export LD_LIBRARY_PATH="$sdk_prefix/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
tags=()
if [[ "$profile" != platform ]]; then tags=(-tags "reflow_$profile"); fi
if [[ "$profile" == pfring && "$PF_RING_ZC" == 1 ]]; then tags=(-tags reflow_pfring,reflow_pfring_zc); fi
go test "${tags[@]}" ./pkg/reflow/... ./cmd/reflow/...
go vet "${tags[@]}" ./pkg/reflow/... ./cmd/reflow/...
make REFLOW_PROFILE="$profile" package-reflow-tar
binary=$(make --no-print-directory REFLOW_PROFILE="$profile" print-reflow-output)
python3 scripts/reflow-verify-profile.py "$binary" linux "$arch" "$profile"

# Record the loader dependencies and enforce that profiles do not acquire an
# unrelated packet-capture library through their SDKs.
readelf -d "$binary" > "$binary.dependencies.txt"
if [[ "$profile" != pcap ]] && ldd "$binary" 2>/dev/null | grep -q libpcap; then
  echo "libpcap dependency leaked into $profile profile" >&2
  exit 1
fi
if [[ "$profile" == platform ]] && readelf -l "$binary" | grep -q INTERP; then
  echo "platform profile must remain cgo-free and statically linked" >&2
  exit 1
fi

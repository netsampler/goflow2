#!/usr/bin/env bash
# Run in a Debian Go image. For ARMv7 pcap, use Bookworm's armhf libraries
# with a native cross compiler. ARMv7 DPDK still builds in the armhf image.
set -euo pipefail

profile=${REFLOW_PROFILE:-platform}
build_only=${REFLOW_BUILD_ONLY:-false}
arch=${GOARCH:-$(go env GOARCH)}
case "$profile" in platform|pcap|dpdk) ;; *) echo "unknown Reflow profile: $profile" >&2; exit 1 ;; esac
case "$arch" in amd64|arm64) ;; arm) export GOARM=7 ;; *) echo "unsupported Linux architecture: $arch" >&2; exit 1 ;; esac
export GOOS=linux GOARCH="$arch" GOTOOLCHAIN=local
export DEBIAN_FRONTEND=noninteractive
sdk_prefix=${REFLOW_SDK_PREFIX:-/opt/reflow-sdk}
sdk_cached=${REFLOW_SDK_CACHE_HIT:-false}
sdk_work=$(mktemp -d)
trap 'rm -rf "$sdk_work"' EXIT

pcap_packages=(libpcap-dev)
if [[ "$profile" == pcap && "$arch" == arm && $(dpkg --print-architecture) != armhf ]]; then
  dpkg --add-architecture armhf
  pcap_packages=(gcc-arm-linux-gnueabihf libc6-dev-armhf-cross libpcap0.8-dev:armhf)
  export CC=arm-linux-gnueabihf-gcc
fi
apt-get update -qq
apt-get install -y --no-install-recommends python3 file binutils pkg-config ca-certificates curl xz-utils

case "$profile" in
  platform)
    export CGO_ENABLED=0
    ;;
  pcap)
    apt-get install -y --no-install-recommends "${pcap_packages[@]}"
    export CGO_ENABLED=1
    ;;
  dpdk)
    # The null PMD supplies a hardware-independent integration fixture. Pcap
    # remains disabled even when a build environment happens to provide it.
    apt-get install -y --no-install-recommends meson ninja-build python3-pyelftools libnuma-dev
    version=25.11.4
    if [[ "$arch" == arm ]]; then
      # Meson probes -march=armv7-a before DPDK adds its NEON flags. Debian's
      # hard-float compiler needs the FPU selected during that probe too.
      export CC="${CC:-cc} -mfpu=neon"
    fi
    if [[ "$sdk_cached" != true ]]; then
      mkdir -p "$sdk_work/dpdk"
      curl --fail --location --retry 3 "https://fast.dpdk.org/rel/dpdk-${version}.tar.xz" -o "$sdk_work/dpdk.tar.xz"
      tar -C "$sdk_work/dpdk" --strip-components=1 -xf "$sdk_work/dpdk.tar.xz"
      meson setup "$sdk_work/dpdk/build" "$sdk_work/dpdk" \
        --prefix="$sdk_prefix" --libdir=lib -Dplatform=generic \
        -Ddefault_library=shared -Dtests=false -Dexamples= -Ddisable_drivers=net/pcap
      ninja -C "$sdk_work/dpdk/build" -j "$(nproc)"
      ninja -C "$sdk_work/dpdk/build" install
    else
      echo "Using cached DPDK SDK"
    fi
    export CGO_ENABLED=1
    export PKG_CONFIG_PATH="$sdk_prefix/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
    # DPDK exposes these architecture-specific GCC options through pkg-config.
    export CGO_CFLAGS_ALLOW='-m(rtm|outline-atomics)'
    export REFLOW_TEST_DPDK=1
    ;;
esac

export LD_LIBRARY_PATH="$sdk_prefix/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
tags=()
if [[ "$profile" != platform ]]; then tags=(-tags "reflow_$profile"); fi
if [[ "$build_only" != true ]]; then
  go test "${tags[@]}" ./pkg/reflow/... ./cmd/reflow/...
fi
go vet "${tags[@]}" ./pkg/reflow/... ./cmd/reflow/...
make REFLOW_PROFILE="$profile" package-reflow-tar
binary=$(make --no-print-directory REFLOW_PROFILE="$profile" print-reflow-output)
if [[ "$build_only" != true || "$profile" == dpdk ]]; then
  python3 scripts/reflow-verify-profile.py "$binary" linux "$arch" "$profile"
fi

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

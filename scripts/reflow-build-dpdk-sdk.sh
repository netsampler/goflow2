#!/usr/bin/env bash
# Sourced by the Linux profile build so CC also applies to ReFlow's cgo build.
apt-get install -y --no-install-recommends meson ninja-build python3-pyelftools libnuma-dev

if [[ "$GOARCH" == arm ]]; then
  # Meson probes -march=armv7-a before DPDK adds its NEON flags.
  export CC="${CC:-cc} -mfpu=neon"
fi

if [[ "${REFLOW_SDK_CACHE_HIT:-false}" == true ]]; then
  echo "Using cached DPDK SDK"
else
  (
    version=25.11.4
    sdk_work=$(mktemp -d)
    trap 'rm -rf "$sdk_work"' EXIT
    mkdir -p "$sdk_work/dpdk"
    curl --fail --location --retry 3 "https://fast.dpdk.org/rel/dpdk-${version}.tar.xz" -o "$sdk_work/dpdk.tar.xz"
    tar -C "$sdk_work/dpdk" --strip-components=1 -xf "$sdk_work/dpdk.tar.xz"
    # Keep the null PMD for tests and exclude libpcap from this profile.
    meson setup "$sdk_work/dpdk/build" "$sdk_work/dpdk" \
      --prefix="$1" --libdir=lib -Dplatform=generic \
      -Ddefault_library=shared -Dtests=false -Dexamples= -Ddisable_drivers=net/pcap
    ninja -C "$sdk_work/dpdk/build" -j "$(nproc)"
    ninja -C "$sdk_work/dpdk/build" install
  )
fi

#!/usr/bin/env bash
# Cross-build main-branch artifacts against the BSD releases used by native CI.
set -euo pipefail

export GOOS="$REFLOW_GOOS" GOARCH="$REFLOW_GOARCH" CGO_ENABLED=1 GOTOOLCHAIN=local
export VERSION="$REFLOW_VERSION" REFLOW_PROFILE=pcap
sysroot=${REFLOW_BSD_SYSROOT:?}
case "$GOARCH" in
  amd64) target_arch=x86_64; freebsd_path=amd64/amd64; netbsd_path=amd64 ;;
  arm64) target_arch=aarch64; freebsd_path=arm64/aarch64; netbsd_path=evbarm-aarch64 ;;
  *) echo "Unsupported BSD architecture: $GOARCH" >&2; exit 1 ;;
esac

make_args=()
link_mode=
case "$GOOS/$GOARCH" in
  freebsd/amd64|freebsd/arm64)
    url="https://archive.freebsd.org/old-releases/$freebsd_path/14.3-RELEASE"
    target="$target_arch-unknown-freebsd14.3"
    interpreter=/libexec/ld-elf.so.1
    if [[ "$GOARCH" == arm64 ]]; then
      # Go's external linker requires FreeBSD-targeted GNU ld.bfd here.
      hashes=('f83e824cb7a20dbadb2888a8bd253e6a1ac35024cc6dc8af9f3e229f75ec7129 base.txz')
      link_mode=internal
    else
      hashes=('e38b5cf756d60086a6c2f736eff19cc7685f7e2313e31d14342fc8df57200a92 base.txz')
    fi
    ;;
  openbsd/amd64)
    url=https://cdn.openbsd.org/pub/OpenBSD/7.8/amd64
    target="$target_arch-unknown-openbsd7.8"
    interpreter=/usr/libexec/ld.so
    hashes=(
      '2f7a6fba6c6448b95a3118099bc71b832b4b7c7c5a7f97418e443546fa6e6243 base78.tgz'
      'a2a8a6f9b83e4e43e609e7ef4cb22c676f4e6fcfb9407ea566ed31a8021d386f comp78.tgz'
    )
    # The Go external link passes OpenBSD's -nopie flag, which upstream LLD rejects.
    link_mode=internal
    ;;
  netbsd/amd64|netbsd/arm64)
    url="https://archive.netbsd.org/pub/NetBSD-archive/NetBSD-10.1/$netbsd_path/binary/sets"
    target="$target_arch-unknown-netbsd10.1"
    interpreter=/libexec/ld.elf_so
    if [[ "$GOARCH" == arm64 ]]; then
      hashes=(
        '8ad3350f88ab49f3f9da568e8d42abf61d51079e5797efc267739d6c810ad985 base.tar.xz'
        '5528dccd83ffcd47ad0078dc3d980f6451f2a91c4361525103d387d17c46b6a4 comp.tar.xz'
      )
    else
      hashes=(
        'ec59d1198dad7de81771bfb93f50425241bc51331b4c50ba7a163ca4d6acd427 base.tar.xz'
        '02e51e63e05b54f9d30d4d566c55e96e1b8b36fb8e870b1cc3ed9494d93d11f3 comp.tar.xz'
      )
    fi
    ;;
  *) echo "Unsupported BSD pcap cross-build: $GOOS/$GOARCH" >&2; exit 1 ;;
esac

if [[ ! -d "$sysroot" ]]; then
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  mkdir "$work/sysroot"
  for entry in "${hashes[@]}"; do
    read -r checksum archive <<< "$entry"
    curl --fail --silent --show-error --location --retry 3 "$url/$archive" -o "$work/$archive"
    printf '%s  %s\n' "$checksum" "$work/$archive" | sha256sum --check
    tar --warning=no-unknown-keyword -tf "$work/$archive" | awk '/^\.\/(lib|usr\/(include|lib))\// { print }' > "$work/paths"
    tar --warning=no-unknown-keyword -xf "$work/$archive" -C "$work/sysroot" --no-same-owner --no-recursion -T "$work/paths"
    rm "$work/$archive"
  done
  if [[ "$GOOS" == openbsd ]]; then
    # Upstream LLD looks for unversioned linker names.
    for library in c pthread pcap; do
      versioned=("$work/sysroot/usr/lib/lib$library.so."*)
      ln -s "${versioned[0]##*/}" "$work/sysroot/usr/lib/lib$library.so"
    done
  fi
  mv "$work/sysroot" "$sysroot"
fi

export CC="clang-19 --target=$target --sysroot=$sysroot --ld-path=/usr/bin/ld.lld-19 -B$sysroot/usr/lib -L$sysroot/usr/lib -L$sysroot/lib -Qunused-arguments"
go vet -tags reflow_pcap ./pkg/reflow/... ./cmd/reflow/...
if [[ "$link_mode" == internal ]]; then
  make_args+=("LDFLAGS='-linkmode=internal -X main.version=$VERSION -X \"main.buildinfos= ($(date +%FT%T%z))\"'")
fi
make package-reflow-tar "${make_args[@]}"
binary=$(make --no-print-directory print-reflow-output)
readelf -l "$binary" | grep -F "Requesting program interpreter: $interpreter"
readelf -d "$binary" > "$binary.dependencies.txt"
grep -F 'Shared library: [libpcap.so.' "$binary.dependencies.txt"
printf '%s\n' "$binary" > dist/bsd-pcap-output

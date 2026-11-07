#!/bin/sh
# Run inside a native BSD guest, including cgo compilation against its libpcap.
set -eu

toolchain=$(awk '$1 == "toolchain" { print $2 }' go.mod)
test -n "$toolchain"
curl -fsSL "https://go.dev/dl/${toolchain}.${REFLOW_GOOS}-${REFLOW_GOARCH}.tar.gz" -o /tmp/reflow-go.tar.gz
mkdir -p /tmp/reflow-toolchain
tar -xzf /tmp/reflow-go.tar.gz -C /tmp/reflow-toolchain
export PATH="/tmp/reflow-toolchain/go/bin:/usr/local/bin:/usr/pkg/bin:$PATH"
export GOOS="$REFLOW_GOOS" GOARCH="$REFLOW_GOARCH" GOTOOLCHAIN=local
export VERSION="$REFLOW_VERSION" REFLOW_PROFILE
tags=""
CGO_ENABLED=0
expected=bpf
if [ "$REFLOW_PROFILE" = pcap ]; then
    tags=reflow_pcap
    CGO_ENABLED=1
    expected=pcap_live
fi
export CGO_ENABLED
go test -tags "$tags" ./pkg/reflow/...
go vet -tags "$tags" ./pkg/reflow/... ./cmd/reflow/...
gmake package-reflow-tar SHA256_CMD='sha256 -r'
output=$(gmake print-reflow-output)
"$output" -version | tee "${output}.manifest.txt"
grep -F "profile=$REFLOW_PROFILE" "${output}.manifest.txt"
grep -F "capture=[$expected]" "${output}.manifest.txt"
ldd "$output" > "${output}.dependencies.txt" 2>&1 || true
if [ "$REFLOW_PROFILE" = pcap ]; then
    grep -i libpcap "${output}.dependencies.txt"
elif grep -qi libpcap "${output}.dependencies.txt"; then
    echo 'platform profile unexpectedly links libpcap' >&2
    exit 1
fi

#!/bin/sh
# Build the platform profile on the runner and validate both profiles in BSD.
set -eu

export GOOS="$REFLOW_GOOS" GOARCH="$REFLOW_GOARCH" GOTOOLCHAIN=local
export VERSION="$REFLOW_VERSION" REFLOW_PROFILE
test_dir=dist/bsd-tests
root=$(pwd -P)

if [ "${1:-}" = cross-build ]; then
    export CGO_ENABLED=0
    mkdir -p "$test_dir"
    go test -c -o "$test_dir/" ./pkg/reflow/...
    packages=$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.Dir}}{{end}}' ./pkg/reflow/...)
    # Record workspace-relative directories for running the tests in the guest.
    while IFS= read -r package; do
        [ -n "$package" ] || continue
        printf '%s\n' "${package#"$root"/}"
    done > "$test_dir/packages" <<EOF
$packages
EOF
    go vet ./pkg/reflow/... ./cmd/reflow/...
    make package-reflow-tar
    make --no-print-directory print-reflow-output > "$test_dir/output"
    exit 0
fi

if [ "$REFLOW_PROFILE" = pcap ]; then
    toolchain=$(awk '$1 == "toolchain" { print $2 }' go.mod)
    test -n "$toolchain"
    curl -fsSL "https://go.dev/dl/${toolchain}.${REFLOW_GOOS}-${REFLOW_GOARCH}.tar.gz" -o /tmp/reflow-go.tar.gz
    mkdir -p /tmp/reflow-toolchain
    tar -xzf /tmp/reflow-go.tar.gz -C /tmp/reflow-toolchain
    export PATH="/tmp/reflow-toolchain/go/bin:/usr/local/bin:/usr/pkg/bin:$PATH"
    export CGO_ENABLED=1
    go test -tags reflow_pcap ./pkg/reflow/...
    go vet -tags reflow_pcap ./pkg/reflow/... ./cmd/reflow/...
    sha256_cmd='sha256 -r'
    if [ "$REFLOW_GOOS" = netbsd ]; then
        sha256_cmd='sha256 -n'
    fi
    gmake package-reflow-tar SHA256_CMD="$sha256_cmd"
    output=$(gmake print-reflow-output)
    expected=pcap_live
else
    while IFS= read -r package; do
        echo "Testing $package"
        (cd "$package" && "$root/$test_dir/${package##*/}.test" -test.timeout=10m -test.paniconexit0)
    done < "$test_dir/packages"
    output=$(cat "$test_dir/output")
    expected=bpf
fi
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

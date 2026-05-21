#!/usr/bin/env sh
set -eu

usage() {
	cat <<'EOF'
Usage: scripts/reflow-profile.sh [preset] [out-dir] [benchtime]

Presets:
  processor-json   Profile JSON-heavy processor benchmarks.
  encode           Profile ReFlow encoder benchmarks.
  pipeline         Profile in-memory app pipeline benchmarks.
  aggregate        Profile stateful aggregation benchmarks.
  decode           Profile decode benchmarks.
  all              Run all presets.

Defaults:
  preset     processor-json
  out-dir    /tmp/reflow-profiles
  benchtime  5s

The script always writes standard Go CPU and memory profiles. If go tool pprof is
available, it also writes top reports. On toolchains that only expose
go tool preprofile, it writes the converted preprofile text instead.
EOF
}

preset="${1:-processor-json}"
case "$preset" in
	-h|--help)
		usage
		exit 0
		;;
esac

out_root="${2:-/tmp/reflow-profiles}"
benchtime="${3:-5s}"
mkdir -p "$out_root"

run_profile() {
	name="$1"
	pkg="$2"
	bench="$3"
	out_dir="$out_root/$name"
	mkdir -p "$out_dir"

	cpu_profile="$out_dir/cpu.pprof"
	mem_profile="$out_dir/mem.pprof"
	bench_log="$out_dir/bench.txt"

	echo "==> profiling $name"
	go test "$pkg" \
		-run '^$' \
		-bench "$bench" \
		-benchmem \
		-benchtime "$benchtime" \
		-count=1 \
		-cpuprofile "$cpu_profile" \
		-memprofile "$mem_profile" | tee "$bench_log"

	if go tool pprof -h >/dev/null 2>&1; then
		go tool pprof -top -nodecount=30 "$cpu_profile" > "$out_dir/cpu-top.txt"
		go tool pprof -top -alloc_space -nodecount=30 "$mem_profile" > "$out_dir/mem-alloc-top.txt"
	elif go tool preprofile -V >/dev/null 2>&1; then
		if ! go tool preprofile -i "$cpu_profile" -o "$out_dir/cpu.preprofile.txt"; then
			echo "    preprofile could not convert CPU profile; kept $cpu_profile" >&2
		fi
		if ! go tool preprofile -i "$mem_profile" -o "$out_dir/mem.preprofile.txt"; then
			echo "    preprofile could not convert memory profile; kept $mem_profile" >&2
		fi
	fi

	echo "    wrote $out_dir"
}

case "$preset" in
	processor-json)
		run_profile processor-json ./internal/reflow/processor 'BenchmarkBuiltinProcess/(goflow2v2_json|reflow_json)'
		;;
	encode)
		run_profile encode ./internal/reflow/encode 'BenchmarkEncode'
		;;
	pipeline)
		run_profile pipeline ./internal/reflow/app 'BenchmarkRunPipeline'
		;;
	aggregate)
		run_profile aggregate ./internal/reflow/aggregate 'BenchmarkStateful(Process|Close)'
		;;
	decode)
		run_profile decode ./internal/reflow/decode 'BenchmarkDecode'
		;;
	all)
		run_profile processor-json ./internal/reflow/processor 'BenchmarkBuiltinProcess/(goflow2v2_json|reflow_json)'
		run_profile encode ./internal/reflow/encode 'BenchmarkEncode'
		run_profile pipeline ./internal/reflow/app 'BenchmarkRunPipeline'
		run_profile aggregate ./internal/reflow/aggregate 'BenchmarkStateful(Process|Close)'
		run_profile decode ./internal/reflow/decode 'BenchmarkDecode'
		;;
	*)
		echo "unknown preset: $preset" >&2
		usage >&2
		exit 2
		;;
esac

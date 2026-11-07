package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCaptureInputGeneration(t *testing.T) {
	for _, spec := range []string{"af_packet:eth0:bytes?sample_every=10", "xdp:eth0:bytes", "nflog:42:bytes", "psample:1,2:bytes", "pktap:all:bytes", "dpdk:0000:01:00.0:bytes", "pfring_zc:ipc:7:2:bytes"} {
		t.Run(spec, func(t *testing.T) {
			s, err := parseInputSpec(spec)
			if err != nil {
				t.Fatal(err)
			}
			if err := NormalizeCapture(&s); err != nil {
				t.Fatal(err)
			}
			raw, err := yaml.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), "capture:") {
				t.Fatalf("missing capture block: %s", raw)
			}
			if strings.Contains(string(raw), "\ninterface:") || strings.Contains(string(raw), "\nsnaplen:") {
				t.Fatalf("flat capture fields in generated YAML: %s", raw)
			}
			var roundtrip SourceConfig
			if err := yaml.Unmarshal(raw, &roundtrip); err != nil {
				t.Fatal(err)
			}
			if err := NormalizeCapture(&roundtrip); err != nil {
				t.Fatal(err)
			}
			if roundtrip.Capture.SnapLen != s.Capture.SnapLen || roundtrip.Capture.Interface != s.Capture.Interface {
				t.Fatal("capture config changed after YAML round trip")
			}
		})
	}
}

func TestCaptureSamplingOffsetSurvivesLegacyDefaults(t *testing.T) {
	s := SourceConfig{
		Network: "af_packet", Interface: "eth0", SampleEvery: 10,
		Capture: CaptureConfig{SampleOffset: 3},
	}
	if err := NormalizeCapture(&s); err != nil {
		t.Fatal(err)
	}
	if s.Capture.SampleEvery != 10 || s.Capture.SampleOffset != 3 {
		t.Fatalf("capture sampling offset was overwritten: %#v", s.Capture)
	}
}

func TestCaptureKernelSamplingRoundTrip(t *testing.T) {
	var s SourceConfig
	if err := yaml.Unmarshal([]byte(`network: af_packet
capture:
  interface: eth0
  sampling:
    location: kernel
    every: 16
`), &s); err != nil {
		t.Fatal(err)
	}
	if err := NormalizeCapture(&s); err != nil {
		t.Fatal(err)
	}
	if s.Capture.SampleEvery != 16 || s.Capture.Sampling.Algorithm != "random" {
		t.Fatalf("sampling config: %+v", s.Capture)
	}
	raw, err := yaml.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var again SourceConfig
	if err := yaml.Unmarshal(raw, &again); err != nil {
		t.Fatal(err)
	}
	if err := NormalizeCapture(&again); err != nil {
		t.Fatal(err)
	}
	if again.Capture.Sampling != s.Capture.Sampling || again.Capture.SampleEvery != 16 {
		t.Fatalf("sampling changed after round trip: %+v", again.Capture)
	}
}

func TestEBPFTransportRoundTrip(t *testing.T) {
	for _, transport := range []string{"perf", "ringbuf", "auto"} {
		t.Run(transport, func(t *testing.T) {
			s, err := parseInputSpec("ebpf:eth0:bytes?transport=" + transport + "&ring_size=65536")
			if err != nil {
				t.Fatal(err)
			}
			if err := NormalizeCapture(&s); err != nil {
				t.Fatal(err)
			}
			raw, err := yaml.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var again SourceConfig
			if err := yaml.Unmarshal(raw, &again); err != nil {
				t.Fatal(err)
			}
			if err := NormalizeCapture(&again); err != nil {
				t.Fatal(err)
			}
			if again.Capture.EBPF.Transport != transport || again.Capture.EBPF.RingSize != 65536 {
				t.Fatalf("transport config changed: %+v", again.Capture.EBPF)
			}
		})
	}
}

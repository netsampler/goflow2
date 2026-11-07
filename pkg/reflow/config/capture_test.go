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

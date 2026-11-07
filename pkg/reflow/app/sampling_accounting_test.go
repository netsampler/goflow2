package app

import (
	"flag"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/aggregate"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/decode"
	"github.com/netsampler/goflow2/v3/pkg/reflow/encode"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

// Options may arrive before or after data at a different rate. Each data record
// must remain independently weightable after aggregation and templated export.
func TestSamplingRatesRemainSeparateThroughTemplatedExports(t *testing.T) {
	for _, network := range []string{"af_packet", "pktmon"} {
		for _, format := range []string{"ipfix", "netflowv9"} {
			t.Run(network+"/"+format, func(t *testing.T) { testSamplingRatesRemainSeparate(t, network, format) })
		}
	}
}

func testSamplingRatesRemainSeparate(t *testing.T, network, format string) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := config.BindFlags(fs)
	if err := fs.Parse([]string{"-input=" + network + ":eth2:bytes", "-output=" + format + ":udp:[::1]:4739", "-agg", "-genconf"}); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.LoadFromFlags(flags)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := encode.New(cfg.Encoder)
	if err != nil {
		t.Fatal(err)
	}
	dec := decode.NewWithCatalog(cfg.TemplatedFields.Catalog)
	defer dec.Close()
	var weightedBytes, rawBytes int64
	rates := map[uint32]int{}
	const losses = uint64(1)<<40 + 123
	lossSeen := false
	consume := func(payloads [][]byte) {
		t.Helper()
		for _, payload := range payloads {
			rows, err := dec.Decode(&event.Event{Payload: payload, Source: event.SourceMetadata{Type: "flow", Remote: "[::1]:54408"}})
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if value, ok := row.Fields["capture_drops_total"]; ok {
					if value != losses {
						t.Fatalf("capture loss = %#v, want %d", value, losses)
					}
					lossSeen = true
				}
				if row.Fields["record_kind"] != "packet" {
					continue
				}
				rate, ok := row.Fields["sampling_rate"].(uint32)
				if !ok {
					t.Fatalf("missing per-record sampling rate: %#v", row.Fields)
				}
				b := row.Fields["bytes"].(int64)
				rates[rate]++
				rawBytes += b
				weightedBytes += b * int64(rate)
			}
		}
	}
	send := func(evt *event.Event) {
		t.Helper()
		payloads, err := enc.Encode(evt)
		if err != nil {
			t.Fatal(err)
		}
		consume(payloads)
	}
	for i, aggCfg := range cfg.Aggregators {
		agg, err := aggregate.New(aggCfg)
		if err != nil {
			t.Fatal(err)
		}
		controls, err := agg.InitEvents()
		if err != nil {
			t.Fatal(err)
		}
		for _, evt := range controls {
			send(evt)
		}
		if i == 0 {
			for _, rate := range []uint32{4, 16, 4} {
				_, err := agg.Process(&event.Event{ReceivedAt: time.Now(), Source: event.SourceMetadata{SourceID: 7, SourceIDSet: true, Sampling: &event.SamplingMetadata{Rate: rate}}, Fields: map[string]any{"record_kind": "packet", "src_addr": "192.0.2.1", "dst_addr": "198.51.100.1", "src_port": uint32(12345), "dst_port": uint32(443), "proto": uint32(6), "bytes": int64(1000), "packets": int64(1)}})
				if err != nil {
					t.Fatal(err)
				}
			}
			rows, err := agg.Close()
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				send(row)
			}
		} else {
			rows, err := agg.Process(&event.Event{ReceivedAt: time.Now(), Kind: "control", Control: &event.ControlMetadata{Type: "source_init"}, Source: event.SourceMetadata{SourceID: 7, SourceIDSet: true, Sampling: &event.SamplingMetadata{Rate: 1, CaptureDropsTotal: losses}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				send(row)
			}
		}
	}
	payloads, err := enc.Flush()
	if err != nil {
		t.Fatal(err)
	}
	consume(payloads)
	// Generated IPFIX source options carry the 64-bit capture health field.
	// Both protocols must retain the divisor independently on each data record.
	if len(rates) != 2 || rates[4] != 1 || rates[16] != 1 || rawBytes != 3000 || weightedBytes != 24000 || (format == "ipfix" && !lossSeen) {
		t.Fatalf("rates=%v raw=%d weighted=%d capture_loss_seen=%v", rates, rawBytes, weightedBytes, lossSeen)
	}
}

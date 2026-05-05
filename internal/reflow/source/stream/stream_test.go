package stream

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func TestSourceReadsPcapStream(t *testing.T) {
	path := writePcapFile(t, false)

	src, err := New(config.SourceConfig{
		Network: "stream",
		Address: path,
		Type:    "pcap",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	events := collectEvents(t, src)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	evt := events[0]
	if evt.Source.Type != "bytes" {
		t.Fatalf("expected source type bytes, got %q", evt.Source.Type)
	}
	if evt.Fields["stream_type"] != "pcap" {
		t.Fatalf("expected stream_type pcap, got %#v", evt.Fields["stream_type"])
	}
	if evt.Fields["protocol"] != uint32(1) {
		t.Fatalf("expected ethernet protocol metadata, got %#v", evt.Fields["protocol"])
	}
	if evt.Fields["wire_length"] != 60 {
		t.Fatalf("expected wire_length 60, got %#v", evt.Fields["wire_length"])
	}
	if payload, ok := evt.Payload.([]byte); !ok || len(payload) != 34 {
		t.Fatalf("expected 34 payload bytes, got %T len=%d", evt.Payload, len(payload))
	}
	if !evt.ReceivedAt.Equal(time.Unix(42, 123).UTC()) {
		t.Fatalf("expected pcap timestamp, got %s", evt.ReceivedAt)
	}
}

func TestSourceInitEventsExposeStreamInterfaceName(t *testing.T) {
	src, err := New(config.SourceConfig{
		Network: "stream",
		Address: "/tmp/capture.pipe",
		Type:    "pcap",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	events, err := src.InitEvents()
	if err != nil {
		t.Fatalf("InitEvents returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one init event, got %d", len(events))
	}
	if events[0].Control == nil || events[0].Control.Type != "source_init" {
		t.Fatalf("expected source_init control event, got %#v", events[0].Control)
	}
	if events[0].Source.CaptureInterface != "capture.pipe" {
		t.Fatalf("expected capture interface capture.pipe, got %q", events[0].Source.CaptureInterface)
	}
}

func TestSourceReadsPcapngStream(t *testing.T) {
	path := writePcapFile(t, true)

	src, err := New(config.SourceConfig{
		Network: "stream",
		Address: path,
		Type:    "pcapng",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	events := collectEvents(t, src)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["stream_type"] != "pcapng" {
		t.Fatalf("expected stream_type pcapng, got %#v", events[0].Fields["stream_type"])
	}
	if events[0].Fields["pcap_link_type"] != uint32(layers.LinkTypeEthernet) {
		t.Fatalf("expected ethernet link type, got %#v", events[0].Fields["pcap_link_type"])
	}
}

func TestSourceReadsNDJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.ndjson")
	if err := os.WriteFile(path, []byte("{\"src_addr\":\"192.0.2.1\"}\n{\"dst_addr\":\"198.51.100.2\"}\n"), 0o644); err != nil {
		t.Fatalf("write ndjson: %v", err)
	}

	src, err := New(config.SourceConfig{
		Network: "stream",
		Address: path,
		Type:    "json",
		JSON:    config.JSONConfig{Flavor: "reflow"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	events := collectEvents(t, src)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Source.Type != "json" || events[0].Source.JSON.Flavor != "reflow" {
		t.Fatalf("expected reflow JSON source metadata, got %#v", events[0].Source)
	}
	if !bytes.Contains(events[0].Message, []byte("src_addr")) {
		t.Fatalf("expected first message to contain src_addr, got %s", events[0].Message)
	}
}

func TestSourceReadNDJSONReturnsOnContextCancelWhenReaderBlocks(t *testing.T) {
	src := &Source{}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- src.readNDJSON(ctx, func(*event.Event) error {
			return nil
		}, reader)
	}()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("readNDJSON returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("readNDJSON did not return after context cancellation")
	}
}

func collectEvents(t *testing.T, src *Source) []*event.Event {
	t.Helper()
	var events []*event.Event
	if err := src.Start(context.Background(), func(evt *event.Event) error {
		events = append(events, evt)
		return nil
	}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return events
}

func writePcapFile(t *testing.T, pcapng bool) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "capture")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create capture: %v", err)
	}
	defer f.Close()

	ci := gopacket.CaptureInfo{
		Timestamp:     time.Unix(42, 123).UTC(),
		CaptureLength: 34,
		Length:        60,
	}
	data := make([]byte, 34)
	data[12] = 0x08
	data[13] = 0x00
	data[14] = 0x45

	if pcapng {
		w, err := pcapgo.NewNgWriter(f, layers.LinkTypeEthernet)
		if err != nil {
			t.Fatalf("NewNgWriter: %v", err)
		}
		if err := w.WritePacket(ci, data); err != nil {
			t.Fatalf("WritePacket pcapng: %v", err)
		}
		if err := w.Flush(); err != nil {
			t.Fatalf("Flush pcapng: %v", err)
		}
		return path
	}

	w := pcapgo.NewWriterNanos(f)
	if err := w.WriteFileHeader(65535, layers.LinkTypeEthernet); err != nil {
		t.Fatalf("WriteFileHeader: %v", err)
	}
	if err := w.WritePacket(ci, data); err != nil {
		t.Fatalf("WritePacket pcap: %v", err)
	}
	return path
}

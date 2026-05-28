package sink

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
)

func TestFileSinkFramingNoneAndTruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(path, []byte("old data"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	s, err := New(config.SinkConfig{
		Type:    "file",
		Path:    path,
		Framing: "none",
		Mode:    "truncate",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if err := s.Send([]byte{0, 1, 2}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != string([]byte{0, 1, 2}) {
		t.Fatalf("expected exact binary payload, got %v", got)
	}
}

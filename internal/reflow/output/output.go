package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

type Writer struct {
	mu sync.Mutex
	w  io.Writer
	f  *os.File
}

func New(cfg config.OutputConfig) (*Writer, error) {
	out := &Writer{}
	switch cfg.Type {
	case "stdout":
		out.w = os.Stdout
	case "file":
		f, err := os.OpenFile(cfg.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("open output file %s: %w", cfg.Path, err)
		}
		out.f = f
		out.w = f
	default:
		return nil, fmt.Errorf("unsupported output.type %q", cfg.Type)
	}
	return out, nil
}

func (w *Writer) Write(evt *event.Event) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.w.Write(data); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	if _, err := w.w.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write event newline: %w", err)
	}
	return nil
}

func (w *Writer) Close() error {
	if w.f != nil {
		return w.f.Close()
	}
	return nil
}

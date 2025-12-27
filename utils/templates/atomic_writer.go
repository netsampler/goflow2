// Package templates provides NetFlow/IPFIX template system helpers.
package templates

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// AtomicWriter defines file-backed JSON persistence with atomic writes.
type AtomicWriter interface {
	Read() ([]byte, error)
	WriteAtomic(payload []byte) error
}

type atomicFileWriter struct {
	path string
	mu   *sync.Mutex
}

var (
	atomicFileLocksMu sync.Mutex
	atomicFileLocks   = map[string]*sync.Mutex{}
)

// NewAtomicFileWriter creates an AtomicWriter for a file path.
func NewAtomicFileWriter(path string) AtomicWriter {
	if path == "" {
		return nil
	}
	return &atomicFileWriter{
		path: path,
		mu:   atomicFileLock(path),
	}
}

func (w *atomicFileWriter) Read() ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	payload, err := os.ReadFile(w.path)
	if os.IsNotExist(err) {
		return nil, io.EOF
	}
	return payload, err
}

func (w *atomicFileWriter) WriteAtomic(payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	dir := filepath.Dir(w.path)
	tmpPath := w.path + "_tmp"

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, w.path); err != nil {
		return err
	}

	if err := syncDir(dir); err != nil {
		slog.Warn("error syncing template directory", slog.String("error", err.Error()))
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = dir.Close()
	}()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync dir: %w", err)
	}
	return nil
}

func atomicFileLock(path string) *sync.Mutex {
	atomicFileLocksMu.Lock()
	defer atomicFileLocksMu.Unlock()
	if lock, ok := atomicFileLocks[path]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	atomicFileLocks[path] = lock
	return lock
}

// Package file implements a file/stdout transport.
package file

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/netsampler/goflow2/v3/transport"
)

// FileDriver writes formatted messages to stdout or a file.
type FileDriver struct {
	fileDestination string
	lineSeparator   string
	w               io.Writer
	file            *os.File
	lock            *sync.Mutex
	reloadCh        chan os.Signal
}

// Prepare registers flags for file transport configuration.
func (d *FileDriver) Prepare() error {
	flag.StringVar(&d.fileDestination, "transport.file", "", "File/console output (empty for stdout)")
	flag.StringVar(&d.lineSeparator, "transport.file.sep", "\n", "Line separator")
	// idea: add terminal coloring based on key partitioning (if any)
	return nil
}

func (d *FileDriver) openFile() error {
	file, err := os.OpenFile(d.fileDestination, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open file %s: %w", d.fileDestination, err)
	}
	d.file = file
	d.w = d.file
	return nil
}

// reopen opens a new file at the configured path, then closes the previous
// one. When the open fails, the previous file stays in use.
func (d *FileDriver) reopen() error {
	d.lock.Lock()
	defer d.lock.Unlock()
	old := d.file
	if err := d.openFile(); err != nil {
		return err
	}
	if err := old.Close(); err != nil {
		return fmt.Errorf("close previous file: %w", err)
	}
	return nil
}

// Init initializes the output destination and reload handling.
func (d *FileDriver) Init() error {
	if d.fileDestination == "" {
		d.w = os.Stdout
	} else {
		var err error

		d.lock.Lock()
		err = d.openFile()
		d.lock.Unlock()
		if err != nil {
			return fmt.Errorf("file transport init: %w", err)
		}

		d.reloadCh = make(chan os.Signal, 1)
		signal.Notify(d.reloadCh, syscall.SIGHUP)
		reloadCh := d.reloadCh
		go func() {
			for {
				if _, ok := <-reloadCh; !ok {
					return
				}
				if err := d.reopen(); err != nil {
					slog.Error("error reopening file", slog.String("error", err.Error()))
				}
			}
		}()
	}
	return nil
}

// Send writes a formatted message and separator to the destination. The lock
// covers both writes, so a message is never split by a concurrent Send or by a
// reopen on SIGHUP.
func (d *FileDriver) Send(key, data []byte) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	w := d.w
	if len(data) > 0 {
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("write message: %w", err)
		}
	}
	if d.lineSeparator == "" {
		return nil
	}
	_, err := w.Write([]byte(d.lineSeparator))
	if err != nil {
		return fmt.Errorf("write separator: %w", err)
	}
	return nil
}

// Close closes the output file and stops reload handling.
func (d *FileDriver) Close() error {
	var closeErr error
	if d.fileDestination != "" {
		d.lock.Lock()
		if err := d.file.Close(); err != nil {
			closeErr = fmt.Errorf("close output file: %w", err)
		}
		d.lock.Unlock()
		if d.reloadCh != nil {
			signal.Stop(d.reloadCh)
			close(d.reloadCh)
			d.reloadCh = nil
		}
	}
	return closeErr
}

func init() {
	d := &FileDriver{
		lock: &sync.Mutex{},
	}
	transport.RegisterTransportDriver("file", d)
}

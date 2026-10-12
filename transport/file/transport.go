// Package file implements a file/stdout transport.
package file

import (
	"flag"
	"fmt"
	"io"
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
	lock            *sync.RWMutex
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
				d.lock.Lock()
				if err := d.file.Close(); err != nil {
					d.lock.Unlock()
					return
				}
				err := d.openFile()
				d.lock.Unlock()
				if err != nil {
					return
				}
				// if there is an error, keeps using the old file
			}
		}()
	}
	return nil
}

// Send writes a formatted message and separator to the destination.
//
// The message and separator are written via a single Write() call rather
// than two. With more than one decode worker (see the collector's `workers`
// listen option), multiple goroutines can call Send() concurrently; two
// separate Write() calls for data then separator can interleave with
// another goroutine's writes landing between them, corrupting any framing
// a consumer builds on top of the separator (or on a length-prefixed binary
// format, since the separator no longer reliably follows each record). A
// single Write() call for the combined buffer is atomic with respect to
// other writers on a regular file opened O_APPEND, so concurrent Send()
// calls can no longer interleave mid-record.
//
// The RLock is held for the duration of the write, not just the read of
// d.w: a SIGHUP-triggered reopen (see Init's reload goroutine) takes the
// write lock to Close() the current file and open a new one. If Send()
// released the read lock right after snapshotting w, a reopen could close
// that file between the snapshot and the Write() call, and the write would
// fail with "file already closed" - losing whichever records were in
// flight at the exact moment of rotation. Holding the RLock across the
// write blocks the reopen until in-flight writes finish, and readers
// (multiple Send() calls) can still run concurrently since RLock is
// shared.
func (d *FileDriver) Send(key, data []byte) error {
	d.lock.RLock()
	defer d.lock.RUnlock()
	w := d.w

	if d.lineSeparator == "" {
		if len(data) == 0 {
			return nil
		}
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("write message: %w", err)
		}
		return nil
	}

	buf := make([]byte, 0, len(data)+len(d.lineSeparator))
	buf = append(buf, data...)
	buf = append(buf, d.lineSeparator...)
	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("write message: %w", err)
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
		lock: &sync.RWMutex{},
	}
	transport.RegisterTransportDriver("file", d)
}

// Package file implements a file/stdout transport.
package file

import (
	"flag"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/netsampler/goflow2/v2/transport"
)

// FileDriver writes formatted messages to stdout or a file.
type FileDriver struct {
	fileDestination string
	lineSeparator   string
	w               io.Writer
	file            *os.File
	lock            *sync.RWMutex
	q               chan bool
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
		return err
	}
	d.file = file
	d.w = d.file
	return err
}

// Init initializes the output destination and reload handling.
func (d *FileDriver) Init() error {
	d.q = make(chan bool, 1)

	if d.fileDestination == "" {
		d.w = os.Stdout
	} else {
		var err error

		d.lock.Lock()
		err = d.openFile()
		d.lock.Unlock()
		if err != nil {
			return err
		}

		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGHUP)
		go func() {
			for {
				select {
				case <-c:
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
				case <-d.q:
					return
				}
			}
		}()
	}
	return nil
}

// Send writes a formatted message and separator to the destination.
func (d *FileDriver) Send(key, data []byte) error {
	d.lock.RLock()
	w := d.w
	d.lock.RUnlock()
	if len(data) > 0 {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	if d.lineSeparator == "" {
		return nil
	}
	_, err := w.Write([]byte(d.lineSeparator))
	return err
}

// Close closes the output file and stops reload handling.
func (d *FileDriver) Close() error {
	var closeErr error
	if d.fileDestination != "" {
		d.lock.Lock()
		if err := d.file.Close(); err != nil {
			closeErr = err
		}
		d.lock.Unlock()
		signal.Ignore(syscall.SIGHUP)
	}
	close(d.q)
	return closeErr
}

func init() {
	d := &FileDriver{
		lock: &sync.RWMutex{},
	}
	transport.RegisterTransportDriver("file", d)
}

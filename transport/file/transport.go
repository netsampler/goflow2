// Package file pkg/transport/file/transport.go
package file

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/netsampler/goflow2/v2/transport"
)

type FileDriver struct {
	fileDestination string
	lineSeparator   string
	w               io.Writer
	file            *os.File
	lock            *sync.RWMutex
	q               chan bool
}

func (d *FileDriver) Prepare(_ context.Context) error {
	flag.StringVar(&d.fileDestination, "transport.file", "", "File/console output (empty for stdout)")
	flag.StringVar(&d.lineSeparator, "transport.file.sep", "\n", "Line separator")

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

func (d *FileDriver) Init(_ context.Context) error {
	d.q = make(chan bool)

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

					err := d.file.Close()
					if err != nil {
						log.Default().Println(err)

						return
					}

					err = d.openFile()
					d.lock.Unlock()

					if err != nil {
						return
					}
				case <-d.q:
					return
				}
			}
		}()
	}

	return nil
}

// Send writes the data to the file or stdout, it accepts a context, key (un-used), and data.
func (d *FileDriver) Send(ctx context.Context, _, data []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		d.lock.RLock()
		w := d.w
		d.lock.RUnlock()
		_, err := fmt.Fprint(w, string(data)+d.lineSeparator)

		return err
	}
}

func (d *FileDriver) Close(_ context.Context) error {
	if d.fileDestination != "" {
		d.lock.Lock()

		err := d.file.Close()
		if err != nil {
			log.Default().Println(err)

			return err
		}

		d.lock.Unlock()
		signal.Ignore(syscall.SIGHUP)
	}

	close(d.q)

	return nil
}

func init() {
	d := &FileDriver{
		lock: &sync.RWMutex{},
	}

	transport.RegisterTransportDriver("file", d)
}

package file

import (
	"context"
	"flag"
	"fmt"
	"github.com/netsampler/goflow2/transport"
	"io"
	"os"
)

type FileDriver struct {
	fileDestination string
	w               io.Writer
	file            *os.File
}

func (d *FileDriver) Prepare() error {
	flag.StringVar(&d.fileDestination, "transport.file", "", "File/console output (empty for stdout)")
	// idea: add terminal coloring based on key partitioning (if any)
	return nil
}

func (d *FileDriver) Init(context.Context) error {
	if d.fileDestination == "" {
		d.w = os.Stdout
	} else {
		var err error
		d.file, err = os.OpenFile(d.fileDestination, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		d.w = d.file
	}
	return nil
}

func (d *FileDriver) Send(key, data []byte) error {
	fmt.Fprintln(d.w, string(data))
	return nil
}

func (d *FileDriver) Close(context.Context) error {
	if d.fileDestination != "" {
		d.file.Close()
	}
	return nil
}

func init() {
	d := &FileDriver{}
	transport.RegisterTransportDriver("file", d)
}

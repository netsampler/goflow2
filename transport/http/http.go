package http

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"sync"

	"github.com/netsampler/goflow2/v2/transport"
)

type HTTPDriver struct {
	httpDestination string
	lock            *sync.RWMutex
	q               chan bool
}

func (d *HTTPDriver) Prepare() error {
	flag.StringVar(&d.httpDestination, "transport.http", "", "HTTP endpoint for output")
	return nil
}

func (d *HTTPDriver) Init() error {
	d.q = make(chan bool, 1)
	return nil
}

func (d *HTTPDriver) Send(key, data []byte) error {
	d.lock.RLock()
	httpDestination := d.httpDestination
	d.lock.RUnlock()

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	resp, err := http.Post(httpDestination, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (d *HTTPDriver) Close() error {
	close(d.q)
	return nil
}

func init() {
	d := &HTTPDriver{
		lock: &sync.RWMutex{},
	}
	transport.RegisterTransportDriver("http", d)
}

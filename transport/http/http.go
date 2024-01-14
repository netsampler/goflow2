package http

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"sync"

	"github.com/netsampler/goflow2/v2/transport"
)

type HTTPDriver struct {
	httpDestination     string
	httpAuthHeader      string
	httpAuthCredentials string
	lock                *sync.RWMutex
	q                   chan bool
	batchSize           int
	batchData           [][]byte
}

func (d *HTTPDriver) Prepare() error {
	flag.StringVar(&d.httpDestination, "transport.http.destination", "", "HTTP endpoint for output")
	flag.StringVar(&d.httpAuthHeader, "transport.http.auth.header", "", "HTTP header to set for credentials")
	flag.StringVar(&d.httpAuthCredentials, "transport.http.auth.credentials", "", "credentials for the header")
	flag.IntVar(&d.batchSize, "transport.http.batchSize", 1000, "Batch size for sending records")

	if d.batchSize <= 0 {
		d.batchSize = 1000 // default batch size
	}

	return nil
}

func (d *HTTPDriver) Init() error {
	d.q = make(chan bool, 1)
	d.batchData = make([][]byte, 0, d.batchSize)
	return nil
}

func (d *HTTPDriver) Send(key, data []byte) error {
	d.lock.RLock()
	httpDestination := d.httpDestination
	httpAuthHeader := d.httpAuthHeader
	httpAuthCredentials := d.httpAuthCredentials
	d.lock.RUnlock()

	d.batchData = append(d.batchData, data)
	fmt.Println("batchData len:", len(d.batchData))
	if len(d.batchData) >= d.batchSize {
		jsonData, err := json.Marshal(d.batchData)
		if err != nil {
			return err
		}

		fmt.Println("jsonData:", string(jsonData))

		req, err := http.NewRequest("POST", httpDestination, bytes.NewBuffer(jsonData))
		if err != nil {
			return err
		}

		req.Header.Set("Content-Type", "application/json")
		fmt.Println("httpAuthHeader:", httpAuthHeader)
		fmt.Println("httpAuthCredentials:", httpAuthCredentials)
		req.Header.Set(httpAuthHeader, httpAuthCredentials)

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		// reset batchData
		d.batchData = d.batchData[:0]
	}

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

package http

import (
	"bytes"
	"encoding/json"
	"flag"
	"math"
	"net/http"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/netsampler/goflow2/v2/transport"
)

type HTTPDriver struct {
	httpDestination     string
	httpAuthHeader      string
	httpAuthCredentials string
	lock                *sync.RWMutex
	q                   chan bool
	batchSize           int
	batchData           []map[string]interface{}
}

func (d *HTTPDriver) Prepare() error {
	defaultBatchSize := 1000

	flag.StringVar(&d.httpDestination, "transport.http.destination", "", "HTTP endpoint for output")
	flag.StringVar(&d.httpAuthHeader, "transport.http.auth.header", "", "HTTP header to set for credentials")
	flag.StringVar(&d.httpAuthCredentials, "transport.http.auth.credentials", "", "credentials for the header")
	flag.IntVar(&d.batchSize, "transport.http.batchSize", defaultBatchSize, "Batch size for sending records")

	if d.batchSize <= 0 {
		d.batchSize = defaultBatchSize // default batch size
	}

	return nil
}

func (d *HTTPDriver) Init() error {
	d.q = make(chan bool, 1)
	d.batchData = make([]map[string]interface{}, 0, d.batchSize)
	return nil
}

func (d *HTTPDriver) Send(key, data []byte) error {
	batchData := make(map[string]interface{})
	err := json.Unmarshal(data, &batchData)
	if err != nil {
		return err
	}
	d.batchData = append(d.batchData, batchData)
	if len(d.batchData) >= d.batchSize {
		jsonData, err := json.Marshal(d.batchData)
		if err != nil {
			return err
		}

		maxRetries := 3
		delay := time.Millisecond * 500 // initial delay

		var wg sync.WaitGroup
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := 0; i < maxRetries; i++ {
				req, err := http.NewRequest("POST", d.httpDestination, bytes.NewBuffer(jsonData))
				if err != nil {
					return
				}

				req.Header.Set("Content-Type", "application/json")
				req.Header.Set(d.httpAuthHeader, d.httpAuthCredentials)

				client := &http.Client{}
				resp, err := client.Do(req)
				if err != nil || (resp.StatusCode < 200 || resp.StatusCode >= 300) {
					if i == maxRetries-1 {
						log.Error(err)
						return
					}
					time.Sleep(delay * time.Duration(math.Pow(2, float64(i)))) // exponential backoff
					continue
				}
				defer resp.Body.Close()

				// reset batchData
				d.batchData = d.batchData[:0]
				break
			}
		}()

		wg.Wait()
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

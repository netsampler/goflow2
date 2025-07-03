package splunk

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"sync"

	"github.com/netsampler/goflow2/v2/transport"
)

type SplunkDriver struct {
	splunkEndpoint  string
	splunkToken     string
	splunkTimeField string
	splunkSource    string
	splunkHostField string
	splunkNoVerify  bool
	lock            *sync.RWMutex
	client          *http.Client
}

type SplunkHECEvent struct {
	Time       float64 `json:"time"`
	Host       string  `json:"host"`
	Source     string  `json:"source"`
	Sourcetype string  `json:"sourcetype"`
	Event      string  `json:"event"`
}

func (d *SplunkDriver) Prepare() error {
	flag.StringVar(&d.splunkEndpoint, "transport.splunk.endpoint", "", "URL for the Splunk HEC")
	flag.StringVar(&d.splunkToken, "transport.splunk.token", "", "Splunk HEC Token")
	flag.StringVar(&d.splunkTimeField, "transport.splunk.timefield", "time_flow_start_ns", "Field to use for event time (must be in NS since epoch format)")
	flag.StringVar(&d.splunkSource, "transport.splunk.source", "netflow", "Source to use for the HEC event")
	flag.StringVar(&d.splunkHostField, "transport.splunk.hostfield", "sampler_address", "Field to use for event host")
	flag.BoolVar(&d.splunkNoVerify, "transport.splunk.noverify", true, "Set to true to skip TLS verification")

	return nil
}

func (d *SplunkDriver) Init() error {
	if d.splunkNoVerify {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		d.client = &http.Client{Transport: tr}
	} else {
		d.client = &http.Client{}
	}
	return nil
}

func (d *SplunkDriver) Send(key, data []byte) error {
	d.lock.RLock()

	// If the client has disconnected, reconnect it
	if d.client == nil {
		if err := d.Init(); err != nil {
			d.lock.RUnlock()
			return fmt.Errorf("SplunkHEC Initialization Error: %v", err)
		}
	}

	// In order to set up fields required, we need to unmarshal the byte array back to json
	if !json.Valid([]byte(data)) {
		return fmt.Errorf("Invalid HEC event JSON: " + string(data))
	}
	var result map[string]any
	json.Unmarshal(data, &result)
	eventTime := result[d.splunkTimeField].(float64) / float64(1e9) // Convert from nanoseconds to seconds
	eventHost := result[d.splunkHostField].(string)

	// Generate the new Splunk HEC event and marshal it into JSON
	event := SplunkHECEvent{
		Time:       eventTime,
		Host:       eventHost,
		Source:     d.splunkSource,
		Sourcetype: "json",
		Event:      string(data),
	}
	packed, _ := json.Marshal(event)

	// Generate the required HTTP request with Authorizations
	req, err := http.NewRequest("POST", d.splunkEndpoint, bytes.NewBuffer(packed))
	if err != nil {
		d.lock.RUnlock()
		return fmt.Errorf("SplunkHEC Request Creation Error: %v", err)
	}
	req.Header.Set("Authorization", "Splunk "+d.splunkToken)
	req.Header.Set("Content-Type", "application/json")

	// Send the HTTP request to Splunk HEC
	// We remain quiet if it succeeded.
	resp, err := d.client.Do(req)
	if err != nil {
		d.lock.RUnlock()
		return fmt.Errorf("SplunkHEC POST Error: %v", err)
	}
	defer resp.Body.Close()

	d.lock.RUnlock()
	return err
}

func (d *SplunkDriver) Close() error {
	d.client.CloseIdleConnections()
	return nil
}

func init() {
	d := &SplunkDriver{
		lock: &sync.RWMutex{},
	}
	transport.RegisterTransportDriver("splunk", d)
}

// Package flowtriq implements a goflow2 transport that forwards flow records
// to a Flowtriq API endpoint (ftagent local proxy or cloud API) for DDoS
// detection analysis.
//
// Configuration flags:
//
//	-transport.flowtriq.url          Flowtriq API base URL (default: http://localhost:8765)
//	-transport.flowtriq.batch        Max records per HTTP POST (default: 500)
//	-transport.flowtriq.interval     Flush interval (default: 5s)
//	-transport.flowtriq.timeout      HTTP request timeout (default: 10s)
//	-transport.flowtriq.tls.skip     Skip TLS certificate verification (default: false)
//
// Auth is read from environment variables:
//
//	FLOWTRIQ_API_KEY   Bearer token matching ftagent API key
//	FLOWTRIQ_NODE_UUID Node UUID header value
//
// Both variables must be set for cloud API mode. When targeting a local ftagent
// proxy (default URL) the proxy handles auth forwarding and these can be omitted.
package flowtriq

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/transport"
)

const (
	defaultURL      = "http://localhost:8765"
	defaultBatch    = 500
	defaultInterval = 5 * time.Second
	defaultTimeout  = 10 * time.Second

	envAPIKey   = "FLOWTRIQ_API_KEY"
	envNodeUUID = "FLOWTRIQ_NODE_UUID"

	maxQueueDepth = 10_000 // drop oldest when queue is full
)

// FlowtriqDriver batches JSON flow records and POSTs them to the Flowtriq API.
type FlowtriqDriver struct {
	// flags
	flagURL      string
	flagBatch    int
	flagInterval time.Duration
	flagTimeout  time.Duration
	flagTLSSkip  bool

	// runtime
	client   *http.Client
	apiKey   string
	nodeUUID string
	endpoint string

	mu      sync.Mutex
	buf     []json.RawMessage
	flushCh chan struct{}
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// Prepare registers CLI flags for the Flowtriq transport.
func (d *FlowtriqDriver) Prepare() error {
	flag.StringVar(&d.flagURL, "transport.flowtriq.url", defaultURL,
		"Flowtriq API base URL (e.g. https://app.flowtriq.com or http://localhost:8765)")
	flag.IntVar(&d.flagBatch, "transport.flowtriq.batch", defaultBatch,
		"Maximum flow records per HTTP POST to Flowtriq")
	flag.DurationVar(&d.flagInterval, "transport.flowtriq.interval", defaultInterval,
		"How often to flush the flow record buffer to Flowtriq")
	flag.DurationVar(&d.flagTimeout, "transport.flowtriq.timeout", defaultTimeout,
		"HTTP request timeout for Flowtriq API calls")
	flag.BoolVar(&d.flagTLSSkip, "transport.flowtriq.tls.skip", false,
		"Skip TLS certificate verification for Flowtriq API (not recommended in production)")
	return nil
}

// Init establishes the HTTP client and starts the background flush goroutine.
func (d *FlowtriqDriver) Init() error {
	d.apiKey = os.Getenv(envAPIKey)
	d.nodeUUID = os.Getenv(envNodeUUID)

	// Validate: if targeting non-localhost we require auth credentials
	isLocal := d.flagURL == defaultURL ||
		startsWith(d.flagURL, "http://localhost") ||
		startsWith(d.flagURL, "http://127.")
	if !isLocal && (d.apiKey == "" || d.nodeUUID == "") {
		return fmt.Errorf(
			"flowtriq transport: %s and %s environment variables must be set when using a remote endpoint (%s)",
			envAPIKey, envNodeUUID, d.flagURL)
	}

	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: d.flagTLSSkip, //nolint:gosec // user-controlled flag
	}
	d.client = &http.Client{
		Timeout: d.flagTimeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
	}

	d.endpoint = d.flagURL + "/api/v1/agent/flow-metrics"
	d.buf = make([]json.RawMessage, 0, d.flagBatch)
	d.flushCh = make(chan struct{}, 1)
	d.stopCh = make(chan struct{})

	d.wg.Add(1)
	go d.flusher()

	slog.Info("flowtriq transport: initialized",
		slog.String("endpoint", d.endpoint),
		slog.Int("batch", d.flagBatch),
		slog.Duration("interval", d.flagInterval),
	)
	return nil
}

// Send enqueues a single JSON flow record. Records are sent in batches by the
// background flusher. `key` is ignored (Flowtriq does not require partitioning).
func (d *FlowtriqDriver) Send(key, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	// Validate the incoming bytes are JSON before queuing them.
	if !json.Valid(data) {
		return fmt.Errorf("flowtriq transport: received non-JSON payload (%d bytes); use -format json", len(data))
	}

	d.mu.Lock()
	if len(d.buf) >= maxQueueDepth {
		// Drop the oldest record rather than blocking or OOMing.
		d.buf = d.buf[1:]
	}
	d.buf = append(d.buf, json.RawMessage(append([]byte(nil), data...)))
	shouldFlush := len(d.buf) >= d.flagBatch
	d.mu.Unlock()

	if shouldFlush {
		select {
		case d.flushCh <- struct{}{}:
		default:
		}
	}
	return nil
}

// Close flushes any remaining records and shuts down the background goroutine.
func (d *FlowtriqDriver) Close() error {
	close(d.stopCh)
	d.wg.Wait()

	// Final flush of anything left in the buffer.
	d.mu.Lock()
	remaining := d.drainLocked()
	d.mu.Unlock()

	if len(remaining) > 0 {
		if err := d.post(remaining); err != nil {
			return fmt.Errorf("flowtriq transport: final flush: %w", err)
		}
	}
	return nil
}

// flusher runs in the background and sends batches on timer tick or when the
// batch size threshold is reached.
func (d *FlowtriqDriver) flusher() {
	defer d.wg.Done()
	ticker := time.NewTicker(d.flagInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.flush()
		case <-d.flushCh:
			d.flush()
		case <-d.stopCh:
			return
		}
	}
}

func (d *FlowtriqDriver) flush() {
	d.mu.Lock()
	records := d.drainLocked()
	d.mu.Unlock()

	if len(records) == 0 {
		return
	}
	if err := d.post(records); err != nil {
		slog.Warn("flowtriq transport: send failed",
			slog.Int("records", len(records)),
			slog.String("error", err.Error()),
		)
	}
}

// drainLocked swaps out the current buffer. Caller must hold d.mu.
func (d *FlowtriqDriver) drainLocked() []json.RawMessage {
	if len(d.buf) == 0 {
		return nil
	}
	out := d.buf
	d.buf = make([]json.RawMessage, 0, d.flagBatch)
	return out
}

// post sends a batch of flow records to the Flowtriq API.
func (d *FlowtriqDriver) post(records []json.RawMessage) error {
	payload := struct {
		Records []json.RawMessage `json:"flows"`
		Count   int               `json:"count"`
		Source  string            `json:"source"`
	}{
		Records: records,
		Count:   len(records),
		Source:  "goflow2",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal batch: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), d.flagTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "goflow2-flowtriq-transport/1.0")

	if d.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+d.apiKey)
	}
	if d.nodeUUID != "" {
		req.Header.Set("X-Node-UUID", d.nodeUUID)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d from Flowtriq API", resp.StatusCode)
	}

	slog.Debug("flowtriq transport: flushed",
		slog.Int("records", len(records)),
		slog.Int("status", resp.StatusCode),
	)
	return nil
}

// startsWith is a simple prefix check to avoid importing strings.
func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func init() {
	d := &FlowtriqDriver{}
	transport.RegisterTransportDriver("flowtriq", d)
}

package influxdb

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"flag"

	"github.com/netsampler/goflow2/v2/transport"
	log "github.com/sirupsen/logrus"
)

type InfluxDbDriver struct {
	influxUrl           string
	influxToken         string
	influxOrganization  string
	influxBucket        string
	influxMeasurement   string
	influxPrecision     string
	influxGZip          bool
	influxTLSSkipVerify bool
	influxLogErrors     bool
	batchSize           int
	batchData           []map[string]interface{}
	lock                *sync.RWMutex
	q                   chan bool
	maxRetries          int
	retryDelay          time.Duration
}

func (d *InfluxDbDriver) Prepare() error {
	flag.StringVar(&d.influxUrl, "transport.influxdb.url", "http://localhost:8086", "InfluxDB URL including port")
	flag.StringVar(&d.influxToken, "transport.influxdb.token", "", "InfluxDB API token")
	flag.StringVar(&d.influxOrganization, "transport.influxdb.organization", "", "InfluxDB organization containing bucket")
	flag.StringVar(&d.influxBucket, "transport.influxdb.bucket", "netflow", "InfluxDB bucket used for writing")
	flag.StringVar(&d.influxMeasurement, "transport.influxdb.measurement", "flowdata", "InfluxDB measurement name")
	flag.StringVar(&d.influxPrecision, "transport.influxdb.precision", "ms", "InfluxDB time precision (ns, us, ms, s)")
	flag.BoolVar(&d.influxGZip, "transport.influxdb.gzip", true, "Use GZip compression")
	flag.BoolVar(&d.influxTLSSkipVerify, "transport.influxdb.skiptlsverify", false, "Insecure TLS skip verify")
	flag.BoolVar(&d.influxLogErrors, "transport.influxdb.log.errors", true, "Log InfluxDB write errors")
	flag.IntVar(&d.batchSize, "transport.influxdb.batchSize", 1000, "Batch size for sending records")
	flag.IntVar(&d.maxRetries, "transport.influxdb.maxRetries", 3, "Maximum number of retries for failed requests")
	retryDelay := flag.Int("transport.influxdb.retryDelay", 500, "Initial retry delay in milliseconds")

	if d.batchSize <= 0 {
		d.batchSize = 1000 // default batch size
	}

	if d.maxRetries <= 0 {
		d.maxRetries = 3 // default max retries
	}

	d.retryDelay = time.Duration(*retryDelay) * time.Millisecond

	return nil
}

func (d *InfluxDbDriver) Init() error {
	// Đảm bảo bucket có giá trị
	if d.influxBucket == "" {
		d.influxBucket = "netflow"
	}
	if d.influxOrganization == "" {
		d.influxOrganization = "org"
	}

	d.q = make(chan bool, 1)
	d.batchData = make([]map[string]interface{}, 0, d.batchSize)
	d.lock = &sync.RWMutex{}
	return nil
}

func (d *InfluxDbDriver) Send(key, data []byte) error {
	var flowData map[string]interface{}
	err := json.Unmarshal(data, &flowData)
	if err != nil {
		return err
	}

	d.lock.Lock()
	d.batchData = append(d.batchData, flowData)
	currentBatchSize := len(d.batchData)
	d.lock.Unlock()

	if currentBatchSize >= d.batchSize {
		return d.sendBatch()
	}

	return nil
}

func (d *InfluxDbDriver) sendBatch() error {
	d.lock.Lock()
	if len(d.batchData) == 0 {
		d.lock.Unlock()
		return nil
	}
	batchToSend := make([]map[string]interface{}, len(d.batchData))
	copy(batchToSend, d.batchData)
	d.batchData = d.batchData[:0]
	d.lock.Unlock()

	lines := make([]string, 0, len(batchToSend))
	for _, flow := range batchToSend {
		// Tự map các trường cần thiết, kiểm tra kiểu dữ liệu
		tags := ""
		fields := ""
		var timestamp string

		// Map tags
		if v, ok := flow["Type"].(string); ok && v != "" {
			tags += ",Type=" + v
		}
		if v, ok := flow["SamplerAddress"].(string); ok && v != "" {
			tags += ",SamplerAddress=" + v
		}
		if v, ok := flow["SrcAddr"].(string); ok && v != "" {
			tags += ",SrcAddr=" + v
		}
		if v, ok := flow["DstAddr"].(string); ok && v != "" {
			tags += ",DstAddr=" + v
		}

		// Map fields
		isFirstField := true
		for k, v := range flow {
			// Skip keys already used as tags
			if k == "Type" || k == "SamplerAddress" || k == "SrcAddr" || k == "DstAddr" {
				continue
			}
			// Format value based on type
			var formattedValue string
			switch val := v.(type) {
			case string:
				formattedValue = fmt.Sprintf("\"%s\"", val)
			case bool:
				formattedValue = fmt.Sprintf("%t", val)
			case float64:
				// Nếu là trường thời gian thì dùng làm timestamp
				if k == "TimeFlowStart" || k == "TimeReceived" || k == "TimeFlowEnd" {
					timestamp = fmt.Sprintf(" %d", int64(val))
					continue
				}
				// Nếu là số nguyên, format không có dấu chấm
				if val == float64(int64(val)) {
					formattedValue = fmt.Sprintf("%d", int64(val))
				} else {
					formattedValue = fmt.Sprintf("%f", val)
				}
			case int, int64:
				formattedValue = fmt.Sprintf("%d", val)
			default:
				formattedValue = fmt.Sprintf("\"%v\"", val)
			}
			if isFirstField {
				fields += k + "=" + formattedValue
				isFirstField = false
			} else {
				fields += "," + k + "=" + formattedValue
			}
		}

		// Nếu không có timestamp thì lấy time.Now()
		if timestamp == "" {
			timestamp = fmt.Sprintf(" %d", time.Now().UnixNano()/int64(time.Millisecond))
		}

		// Tạo line protocol
		line := d.influxMeasurement + tags + " " + fields + timestamp
		lines = append(lines, line)
	}

	// Kiểm tra các tham số bắt buộc
	if d.influxBucket == "" {
		return fmt.Errorf("transport error: missing influxdb.bucket")
	}
	if d.influxOrganization == "" {
		return fmt.Errorf("transport error: missing influxdb.organization")
	}
	if d.influxUrl == "" {
		return fmt.Errorf("transport error: missing influxdb.url")
	}

	// Tạo URL với tham số
	apiUrl, err := url.Parse(d.influxUrl)
	if err != nil {
		return fmt.Errorf("transport error: invalid influxdb.url: %v", err)
	}
	apiUrl.Path = "/api/v2/write"
	q := apiUrl.Query()
	q.Set("org", d.influxOrganization)
	q.Set("bucket", d.influxBucket)
	q.Set("precision", d.influxPrecision)
	apiUrl.RawQuery = q.Encode()

	// Chuẩn bị payload
	payload := []byte(strings.Join(lines, "\n"))

	// Nếu bật gzip, nén payload
	if d.influxGZip {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, err := gz.Write(payload)
		gz.Close()
		if err != nil {
			return fmt.Errorf("transport error: gzip failed: %v", err)
		}
		payload = buf.Bytes()
	}

	// Gửi dữ liệu
	req, err := http.NewRequest("POST", apiUrl.String(), bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("transport error: cannot create request: %v", err)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("Authorization", "Token "+d.influxToken)
	if d.influxGZip {
		req.Header.Set("Content-Encoding", "gzip")
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: d.influxTLSSkipVerify,
			},
		},
	}

	var lastErr error
	for i := 0; i < d.maxRetries; i++ {
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("transport error: send failed: %v", err)
			if d.influxLogErrors {
				log.Errorf("Error sending data to InfluxDB: %v", err)
			}
			time.Sleep(d.retryDelay * time.Duration(math.Pow(2, float64(i))))
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("transport error: InfluxDB responded with status code %d", resp.StatusCode)
			if d.influxLogErrors {
				log.Errorf("InfluxDB responded with status code %d", resp.StatusCode)
			}
			time.Sleep(d.retryDelay * time.Duration(math.Pow(2, float64(i))))
			continue
		}
		// Success
		lastErr = nil
		break
	}
	return lastErr
}

func (d *InfluxDbDriver) Close() error {
	// Send final batch
	err := d.sendBatch()
	close(d.q)
	return err
}

func init() {
	d := &InfluxDbDriver{}
	transport.RegisterTransportDriver("influxdb", d)
}

package http

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"

	"github.com/netsampler/goflow2/v2/transport"
)

type HttpDriver struct {
	HttpServer string
	HttpPort   string
	HttpTag    string
	q          chan bool

	errors chan error
}

func (d *HttpDriver) Prepare() error {
	flag.StringVar(&d.HttpServer, "transport.http.server", "localhost", "HTTP Post destination")
	flag.StringVar(&d.HttpPort, "transport.http.port", "8080", "HTTP port")
	flag.StringVar(&d.HttpTag, "transport.http.tag", "", "HTTP tag")
	return nil
}

func (d *HttpDriver) Errors() <-chan error {
	return d.errors
}

func (d *HttpDriver) Init() error {
	d.q = make(chan bool)
	go func() {
		for {
			<-d.q
			return
		}
	}()
	return nil
}

func (d *HttpDriver) Send(key, data []byte) error {
	url := fmt.Sprintf("http://%s:%s/%s", d.HttpServer, d.HttpPort, d.HttpTag)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		fmt.Println("Error creating request:", err)
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return err
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response:%s, Response status:%s, Response body:%s", err, resp.Status, string(body))
		return err
	}
	return nil
}

func (d *HttpDriver) Close() error {
	close(d.q)
	return nil
}

func init() {
	d := &HttpDriver{
		errors: make(chan error),
	}
	transport.RegisterTransportDriver("http", d)
}

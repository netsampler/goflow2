// Package nats pkg/transport/nats/nats.go
package nats

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/netsampler/goflow2/v2/transport"
)

type Driver struct {
	natsURL     string
	subject     string
	stream      string
	tlsCertFile string
	tlsKeyFile  string
	tlsCAFile   string
	tlsInsecure bool
	nc          *nats.Conn
	js          jetstream.JetStream
	q           chan bool
}

func (d *Driver) Prepare(_ context.Context) error {
	flag.StringVar(&d.natsURL, "transport.nats.url", "nats://localhost:4222", "NATS server URL")
	flag.StringVar(&d.stream, "transport.nats.stream", "goflow2", "NATS JetStream stream name")
	flag.StringVar(&d.subject, "transport.nats.subject", "goflow2.messages", "NATS subject for publishing messages")
	flag.StringVar(&d.tlsCertFile, "transport.nats.tls.cert", "", "NATS client certificate file")
	flag.StringVar(&d.tlsKeyFile, "transport.nats.tls.key", "", "NATS client key file")
	flag.StringVar(&d.tlsCAFile, "transport.nats.tls.ca", "", "NATS CA certificate file")
	flag.BoolVar(&d.tlsInsecure, "transport.nats.tls.insecure", false, "Skip TLS verification for NATS")

	return nil
}

func (d *Driver) Init(ctx context.Context) error {
	d.q = make(chan bool)

	tlsConfig, err := d.getTLSConfig()
	if err != nil {
		return &TransportError{Err: fmt.Errorf("failed to configure TLS: %w", err)}
	}

	// Connect to NATS with mTLS
	nc, err := nats.Connect(d.natsURL,
		nats.Secure(tlsConfig),
		nats.ClientCert(d.tlsCertFile, d.tlsKeyFile),
		nats.RootCAs(d.tlsCAFile),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			fmt.Printf("NATS error: %v\n", err)
		}),
		nats.ConnectHandler(func(nc *nats.Conn) {
			fmt.Printf("Connected to NATS: %s\n", nc.ConnectedUrl())
		}),
	)
	if err != nil {
		return &TransportError{Err: fmt.Errorf("failed to connect to NATS: %w", err)}
	}

	d.nc = nc

	// Initialize JetStream
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()

		return &TransportError{Err: fmt.Errorf("failed to create JetStream context: %w", err)}
	}

	d.js = js

	// Ensure the stream exists
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:      d.stream,
		Subjects:  []string{d.subject},
		Retention: jetstream.WorkQueuePolicy,
	})

	if err != nil && !errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {
		nc.Close()

		return &TransportError{Err: fmt.Errorf("failed to create stream: %w", err)}
	}

	return nil
}

func (d *Driver) getTLSConfig() (*tls.Config, error) {
	if d.tlsCertFile == "" || d.tlsKeyFile == "" || d.tlsCAFile == "" {
		if d.tlsInsecure {
			return &tls.Config{InsecureSkipVerify: true}, nil
		}
		return nil, nil // No TLS
	}

	// Load client certificate and key
	cert, err := tls.LoadX509KeyPair(d.tlsCertFile, d.tlsKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Load CA certificate
	caCert, err := os.ReadFile(d.tlsCAFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            caCertPool,
		InsecureSkipVerify: d.tlsInsecure,
		MinVersion:         tls.VersionTLS13,
	}, nil
}

func (d *Driver) Send(ctx context.Context, _, data []byte) error {
	if d.js == nil {
		return &TransportError{Err: fmt.Errorf("NATS driver not initialized")}
	}

	_, err := d.js.Publish(ctx, d.subject, data)
	if err != nil {
		return &TransportError{Err: fmt.Errorf("failed to publish message: %w", err)}
	}

	return nil
}

func (d *Driver) Close(ctx context.Context) error {
	// set a timeout on the context
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if d.nc != nil {
		d.nc.Close()
	}

	close(d.q)

	return nil
}

func init() {
	d := &Driver{}

	transport.RegisterTransportDriver(context.Background(), "nats", d)
}

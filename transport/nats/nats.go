package nats

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/mfreeman451/goflow2/v2/transport"
)

// Driver implements the GoFlow2 transport interface for NATS JetStream.
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
}

// Prepare sets up command-line flags for the NATS transport.
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

// Init initializes the NATS connection and JetStream context.
func (d *Driver) Init(ctx context.Context) error {
	// Validate mTLS configuration
	if (d.tlsCertFile == "" || d.tlsKeyFile == "" || d.tlsCAFile == "") && !d.tlsInsecure {
		return &TransportError{Err: fmt.Errorf("mTLS requires tls.cert, tls.key, and tls.ca flags")}
	}

	// Connect to NATS with mTLS
	opts := []nats.Option{
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			log.Printf("NATS error: %v", err)
		}),
		nats.ConnectHandler(func(nc *nats.Conn) {
			log.Printf("Connected to NATS: %s", nc.ConnectedUrl())
		}),
	}

	if d.tlsCertFile != "" && d.tlsKeyFile != "" {
		opts = append(opts, nats.ClientCert(d.tlsCertFile, d.tlsKeyFile))
	}

	if d.tlsCAFile != "" {
		opts = append(opts, nats.RootCAs(d.tlsCAFile))
	}

	if d.tlsInsecure {
		opts = append(opts, nats.Secure(&tls.Config{InsecureSkipVerify: true}))
	}

	nc, err := nats.Connect(d.natsURL, opts...)
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

	// Ensure the stream exists or update it
	streamCfg := jetstream.StreamConfig{
		Name:      d.stream,
		Subjects:  []string{d.subject},
		Retention: jetstream.WorkQueuePolicy,
	}

	stream, err := js.Stream(ctx, d.stream)
	if err != nil {
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			_, err = js.CreateStream(ctx, streamCfg)
			if err != nil {
				nc.Close()

				return &TransportError{Err: fmt.Errorf("failed to create stream: %w", err)}
			}

			log.Printf("Created JetStream stream %s with subject %s", d.stream, d.subject)
		} else {
			nc.Close()

			return &TransportError{Err: fmt.Errorf("failed to get stream: %w", err)}
		}
	} else {
		// Check and update stream configuration if necessary
		info, err := stream.Info(ctx)
		if err != nil {
			nc.Close()

			return &TransportError{Err: fmt.Errorf("failed to get stream info: %w", err)}
		}

		if !containsSubject(info.Config.Subjects, d.subject) {
			info.Config.Subjects = append(info.Config.Subjects, d.subject)

			_, err = js.UpdateStream(ctx, info.Config)
			if err != nil {
				nc.Close()

				return &TransportError{Err: fmt.Errorf("failed to update stream: %w", err)}
			}

			log.Printf("Updated JetStream stream %s to include subject %s", d.stream, d.subject)
		}

		log.Printf("Using existing JetStream stream %s", d.stream)
	}

	return nil
}

// Send publishes a protobuf message to the NATS JetStream subject.
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

// Close gracefully shuts down the NATS connection.
func (d *Driver) Close(ctx context.Context) error {
	if d.nc != nil {
		err := d.nc.Drain()
		if err != nil {
			log.Printf("Error draining NATS connection: %v", err)

			return err
		}

		d.nc.Close()

		log.Println("NATS connection closed")
	}

	return nil
}

// containsSubject checks if a subject is in a slice of subjects.
func containsSubject(subjects []string, subject string) bool {
	for _, s := range subjects {
		if s == subject {
			return true
		}
	}

	return false
}

func init() {
	d := &Driver{}

	transport.RegisterTransportDriver(context.Background(), "nats", d)
}

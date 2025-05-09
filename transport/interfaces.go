package transport

import "context"

//go:generate mockgen -destination=mock_transport.go -package=transport github.com/netsampler/goflow2/v2/transport TransportDriver,TransportInterface

type TransportDriver interface {
	Prepare(ctx context.Context) error                // Prepare driver (e.g., flag registration)
	Init(ctx context.Context) error                   // Initialize driver (e.g., start connections, open files)
	Close(ctx context.Context) error                  // Close driver (e.g., close connections and files)
	Send(ctx context.Context, key, data []byte) error // Send a formatted message
}

type TransportInterface interface {
	Send(ctx context.Context, key, data []byte) error
}

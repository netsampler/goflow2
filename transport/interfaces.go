package transport

//go:generate mockgen -destination=mock_transport.go -package=transport github.com/netsampler/goflow2/v2/transport TransportDriver,TransportInterface

type TransportDriver interface {
	Prepare() error              // Prepare driver (eg: flag registration)
	Init() error                 // Initialize driver (eg: start connections, open files...)
	Close() error                // Close driver (eg: close connections and files...)
	Send(key, data []byte) error // Send a formatted message
}

type TransportInterface interface {
	Send(key, data []byte) error
}

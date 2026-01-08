// Package transport provides a registry and interfaces for output transports.
package transport

import (
	"fmt"
	"sync"
)

var (
	transportDrivers = make(map[string]TransportDriver)
	lock             = &sync.RWMutex{}

	// ErrTransport is the base error for transport failures.
	ErrTransport = fmt.Errorf("transport error")
)

// DriverTransportError wraps a driver-specific error with its transport name.
type DriverTransportError struct {
	Driver string
	Err    error
}

func (e *DriverTransportError) Error() string {
	return fmt.Sprintf("%s for %s transport", e.Err.Error(), e.Driver)
}

func (e *DriverTransportError) Unwrap() []error {
	return []error{ErrTransport, e.Err}
}

// TransportDriver describes a transport plugin lifecycle and send method.
type TransportDriver interface {
	Prepare() error              // Prepare driver (eg: flag registration)
	Init() error                 // Initialize driver (eg: start connections, open files...)
	Close() error                // Close driver (eg: close connections and files...)
	Send(key, data []byte) error // Send a formatted message
}

// TransportInterface is the minimal interface needed to send payloads.
type TransportInterface interface {
	Send(key, data []byte) error
}

// Transport is a named transport wrapper used by the registry.
type Transport struct {
	TransportDriver
	name string
}

// Close calls the driver Close and wraps errors with transport metadata.
func (t *Transport) Close() error {
	if err := t.TransportDriver.Close(); err != nil {
		return &DriverTransportError{t.name, err}
	}
	return nil
}

// Send forwards data to the driver and wraps errors with transport metadata.
func (t *Transport) Send(key, data []byte) error {
	if err := t.TransportDriver.Send(key, data); err != nil {
		return &DriverTransportError{t.name, err}
	}
	return nil
}

// RegisterTransportDriver registers and prepares a transport under a name.
func RegisterTransportDriver(name string, t TransportDriver) {
	lock.Lock()
	transportDrivers[name] = t
	lock.Unlock()

	if err := t.Prepare(); err != nil {
		panic(err)
	}
}

// FindTransport returns a configured transport by name.
func FindTransport(name string) (*Transport, error) {
	lock.RLock()
	t, ok := transportDrivers[name]
	lock.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w %s not found", ErrTransport, name)
	}

	err := t.Init()
	if err != nil {
		err = &DriverTransportError{name, err}
	}
	return &Transport{t, name}, err
}

// GetTransports returns the list of registered transport names.
func GetTransports() []string {
	lock.RLock()
	defer lock.RUnlock()
	t := make([]string, len(transportDrivers))
	var i int
	for k := range transportDrivers {
		t[i] = k
		i++
	}
	return t
}

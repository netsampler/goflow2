package transport

import (
	"fmt"
	"sync"
)

var (
	transportDrivers = make(map[string]TransportDriver)
	lock             = &sync.RWMutex{}

	ErrTransport = fmt.Errorf("transport error")
)

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

type TransportDriver interface {
	Prepare() error              // Prepare driver (eg: flag registration)
	Init() error                 // Initialize driver (eg: start connections, open files...)
	Close() error                // Close driver (eg: close connections and files...)
	Send(key, data []byte) error // Send a formatted message
}

type TransportInterface interface {
	Send(key, data []byte) error
}

type Transport struct {
	TransportDriver
	name string
}

func (t *Transport) Close() error {
	if err := t.TransportDriver.Close(); err != nil {
		return &DriverTransportError{t.name, err}
	}
	return nil
}

func (t *Transport) Send(key, data []byte) error {
	if err := t.TransportDriver.Send(key, data); err != nil {
		return &DriverTransportError{t.name, err}
	}
	return nil
}

func RegisterTransportDriver(name string, t TransportDriver) {
	lock.Lock()
	transportDrivers[name] = t
	lock.Unlock()

	if err := t.Prepare(); err != nil {
		panic(err)
	}
}

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

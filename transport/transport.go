// Package transport pkg/transport/transport.go
package transport

import (
	"context"
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

type Transport struct {
	TransportDriver
	name string
}

func (t *Transport) Close(ctx context.Context) error {
	if err := t.TransportDriver.Close(ctx); err != nil {
		return &DriverTransportError{t.name, err}
	}

	return nil
}

func (t *Transport) Send(ctx context.Context, key, data []byte) error {
	if err := t.TransportDriver.Send(ctx, key, data); err != nil {
		return &DriverTransportError{t.name, err}
	}

	return nil
}

func RegisterTransportDriver(ctx context.Context, name string, t TransportDriver) {
	lock.Lock()
	transportDrivers[name] = t
	lock.Unlock()

	if err := t.Prepare(ctx); err != nil {
		panic(err)
	}
}

func FindTransport(ctx context.Context, name string) (*Transport, error) {
	lock.RLock()
	t, ok := transportDrivers[name]
	lock.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%w %s not found", ErrTransport, name)
	}

	err := t.Init(ctx)
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

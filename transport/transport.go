package transport

import (
	"fmt"
	"sync"
)

var (
	transportDrivers = make(map[string]TransportDriver)
	lock             = &sync.RWMutex{}
)

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
	driver TransportDriver
}

func (t *Transport) Close() {
	t.driver.Close()
}
func (t *Transport) Send(key, data []byte) error {
	return t.driver.Send(key, data)
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
		return nil, fmt.Errorf("Transport %s not found", name)
	}

	err := t.Init()
	return &Transport{t}, err
}

func GetTransports() []string {
	lock.RLock()
	defer lock.RUnlock()
	t := make([]string, len(transportDrivers))
	var i int
	for k, _ := range transportDrivers {
		t[i] = k
		i++
	}
	return t
}

// Package format provides a registry and interfaces for output formatters.
package format

import (
	"fmt"
	"sync"
)

var (
	formatDrivers = make(map[string]FormatDriver)
	lock          = &sync.RWMutex{}

	// ErrFormat is the base error for formatter failures.
	ErrFormat = fmt.Errorf("format error")
	// ErrNoSerializer indicates a payload does not implement any supported serializer.
	ErrNoSerializer = fmt.Errorf("message is not serializable")
)

// DriverFormatError wraps a driver-specific error with its formatter name.
type DriverFormatError struct {
	Driver string
	Err    error
}

func (e *DriverFormatError) Error() string {
	return fmt.Sprintf("%s for %s format", e.Err.Error(), e.Driver)
}

func (e *DriverFormatError) Unwrap() []error {
	return []error{ErrFormat, e.Err}
}

// FormatDriver describes a formatter plugin lifecycle and output method.
type FormatDriver interface {
	Prepare() error                                  // Prepare driver (eg: flag registration)
	Init() error                                     // Initialize driver (eg: parse keying)
	Format(data interface{}) ([]byte, []byte, error) // Send a message
}

// FormatInterface is the minimal interface needed to format payloads.
type FormatInterface interface {
	Format(data interface{}) ([]byte, []byte, error)
}

// Format is a named formatter wrapper used by the registry.
type Format struct {
	FormatDriver
	name string
}

// Format calls the underlying driver and annotates errors with the driver name.
func (t *Format) Format(data interface{}) ([]byte, []byte, error) {
	key, text, err := t.FormatDriver.Format(data)
	if err != nil {
		err = &DriverFormatError{
			t.name,
			err,
		}
	}
	return key, text, err
}

// RegisterFormatDriver registers and prepares a formatter under a name.
func RegisterFormatDriver(name string, t FormatDriver) {
	lock.Lock()
	formatDrivers[name] = t
	lock.Unlock()

	if err := t.Prepare(); err != nil {
		panic(err)
	}
}

// FindFormat returns a configured formatter by name.
func FindFormat(name string) (*Format, error) {
	lock.RLock()
	t, ok := formatDrivers[name]
	lock.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w %s not found", ErrFormat, name)
	}

	err := t.Init()
	if err != nil {
		err = &DriverFormatError{name, err}
	}
	return &Format{t, name}, err
}

// GetFormats returns the list of registered formatter names.
func GetFormats() []string {
	lock.RLock()
	defer lock.RUnlock()
	t := make([]string, len(formatDrivers))
	var i int
	for k := range formatDrivers {
		t[i] = k
		i++
	}
	return t
}

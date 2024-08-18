package format

import (
	"fmt"
	"sync"
)

var (
	formatDrivers = make(map[string]FormatDriver)
	lock          = &sync.RWMutex{}

	ErrFormat       = fmt.Errorf("format error")
	ErrNoSerializer = fmt.Errorf("message is not serializable")
)

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

type FormatDriver interface {
	Prepare() error                                  // Prepare driver (eg: flag registration)
	Init() error                                     // Initialize driver (eg: parse keying)
	Format(data interface{}) ([]byte, []byte, error) // Send a message
}

type FormatInterface interface {
	Format(data interface{}) ([]byte, []byte, error)
}

type Format struct {
	FormatDriver
	name string
}

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

func RegisterFormatDriver(name string, t FormatDriver) {
	lock.Lock()
	formatDrivers[name] = t
	lock.Unlock()

	if err := t.Prepare(); err != nil {
		panic(err)
	}
}

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

package format

import (
	"fmt"
	"sync"
)

var (
	formatDrivers = make(map[string]FormatDriver)
	lock          = &sync.RWMutex{}

	ErrorFormat       = fmt.Errorf("format error")
	ErrorNoSerializer = fmt.Errorf("message is not serializable")
)

type DriverFormatError struct {
	Driver string
	Err    error
}

func (e *DriverFormatError) Error() string {
	return fmt.Sprintf("%s for %s format", e.Err.Error(), e.Driver)
}

func (e *DriverFormatError) Unwrap() []error {
	return []error{ErrorFormat, e.Err}
}

type FormatDriver interface {
	Name() string                                    // Get the name of the driver
	Prepare() error                                  // Prepare driver (eg: flag registration)
	Init() error                                     // Initialize driver (eg: parse keying)
	Format(data interface{}) ([]byte, []byte, error) // Send a message
}

type FormatInterface interface {
	Format(data interface{}) ([]byte, []byte, error)
}

type Format struct {
	FormatDriver
}

func (t *Format) Format(data interface{}) ([]byte, []byte, error) {
	key, text, err := t.FormatDriver.Format(data)
	if err != nil {
		err = &DriverFormatError{
			t.Name(),
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
		return nil, fmt.Errorf("%w %s not found", ErrorFormat, name)
	}

	err := t.Init()
	if err != nil {
		err = &DriverFormatError{t.Name(), err}
	}
	return &Format{t}, err
}

func GetFormats() []string {
	lock.RLock()
	defer lock.RUnlock()
	t := make([]string, len(formatDrivers))
	var i int
	for k, _ := range formatDrivers {
		t[i] = k
		i++
	}
	return t
}

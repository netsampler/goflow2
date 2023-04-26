package format

import (
	"context"
	"fmt"
	"sync"
)

var (
	formatDrivers = make(map[string]FormatDriver)
	lock          = &sync.RWMutex{}
)

type FormatDriver interface {
	Prepare() error                                  // Prepare driver (eg: flag registration)
	Init(context.Context) error                      // Initialize driver (eg: parse keying)
	Format(data interface{}) ([]byte, []byte, error) // Send a message

	//FormatInterface // set this and remove Format
}

type FormatInterface interface {
	Format(data interface{}) ([]byte, []byte, error)
}

type Format struct {
	driver FormatDriver
}

func (t *Format) Format(data interface{}) ([]byte, []byte, error) {
	return t.driver.Format(data)
}

func RegisterFormatDriver(name string, t FormatDriver) {
	lock.Lock()
	formatDrivers[name] = t
	lock.Unlock()

	if err := t.Prepare(); err != nil {
		panic(err)
	}
}

func FindFormat(ctx context.Context, name string) (*Format, error) {
	lock.RLock()
	t, ok := formatDrivers[name]
	lock.RUnlock()
	if !ok {
		return nil, fmt.Errorf("Format %s not found", name)
	}

	err := t.Init(ctx)
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

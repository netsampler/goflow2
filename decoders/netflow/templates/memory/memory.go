package memory

import (
	"context"
	"github.com/netsampler/goflow2/decoders/netflow/templates"
	"sync"
)

var (
	Driver = &MemoryDriver{}
)

type templateData struct {
	key  *templates.TemplateKey
	data interface{}
}

type MemoryDriver struct {
	lock      *sync.RWMutex
	templates map[string]templateData
}

func (d *MemoryDriver) Prepare() error {
	// could have an expiry
	return nil
}

func (d *MemoryDriver) Init(context.Context) error {
	d.lock = &sync.RWMutex{}
	d.templates = make(map[string]templateData)
	return nil
}

func (d *MemoryDriver) Close(context.Context) error {
	return nil
}

func (d *MemoryDriver) ListTemplates(ctx context.Context, ch chan *templates.TemplateKey) error {
	d.lock.RLock()
	defer d.lock.RUnlock()
	for _, v := range d.templates {
		select {
		case ch <- v.key:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	select {
	case ch <- nil:
	}
	return nil
}

func (d *MemoryDriver) AddTemplate(ctx context.Context, key *templates.TemplateKey, template interface{}) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.templates[key.String()] = templateData{
		key:  key,
		data: template,
	}
	return nil
}

func (d *MemoryDriver) GetTemplate(ctx context.Context, key *templates.TemplateKey) (interface{}, error) {
	d.lock.RLock()
	defer d.lock.RUnlock()
	return d.templates[key.String()].data, nil
}

func init() {
	templates.RegisterTemplateDriver("memory", Driver)
}

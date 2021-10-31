package memory

import (
	"context"
	"fmt"
	"github.com/netsampler/goflow2/decoders/netflow/templates"
	"sync"
)

type templateData struct {
	version     uint16
	obsDomainId uint32
	templateId  uint16
	data        interface{}
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

func (d *MemoryDriver) key(templateKey string, version uint16, obsDomainId uint32, templateId uint16) string {
	return fmt.Sprintf("%s-%d-%d-%d", templateKey, version, obsDomainId, templateId)
}

func (d *MemoryDriver) AddTemplate(ctx context.Context, templateKey string, version uint16, obsDomainId uint32, templateId uint16, template interface{}) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	key := d.key(templateKey, version, obsDomainId, templateId)
	d.templates[key] = templateData{
		version:     version,
		obsDomainId: obsDomainId,
		templateId:  templateId,
		data:        template,
	}
	return nil
}

func (d *MemoryDriver) GetTemplate(ctx context.Context, templateKey string, version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	d.lock.RLock()
	defer d.lock.RUnlock()
	key := d.key(templateKey, version, obsDomainId, templateId)
	return d.templates[key].data, nil
}

func init() {
	d := &MemoryDriver{}
	templates.RegisterTemplateDriver("memory", d)
}

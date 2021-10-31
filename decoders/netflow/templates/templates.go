package templates

import (
	"context"
	"fmt"
	"sync"
)

var (
	templateDrivers = make(map[string]TemplateDriver) // might be better to change into "factory"
	lock            = &sync.RWMutex{}
)

type TemplateDriver interface {
	TemplateInterface

	Prepare() error              // Prepare driver (eg: flag registration)
	Init(context.Context) error  // Initialize driver (eg: parse keying)
	Close(context.Context) error // Close drive (eg: close file)
}

type TemplateInterface interface {
	//ListTemplate(ctx context.Context) error // to complete
	GetTemplate(ctx context.Context, templateKey string, version uint16, obsDomainId uint32, templateId uint16) (interface{}, error)
	AddTemplate(ctx context.Context, templateKey string, version uint16, obsDomainId uint32, templateId uint16, template interface{}) error // add expiration
}

type TemplateSystem struct {
	driver TemplateDriver
}

func (t *TemplateSystem) AddTemplate(ctx context.Context, templateKey string, version uint16, obsDomainId uint32, templateId uint16, template interface{}) error {
	return t.driver.AddTemplate(ctx, templateKey, version, obsDomainId, templateId, template)
}

func (t *TemplateSystem) GetTemplate(ctx context.Context, templateKey string, version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	return t.driver.GetTemplate(ctx, templateKey, version, obsDomainId, templateId)
}

func (t *TemplateSystem) Close(ctx context.Context) error {
	return t.driver.Close(ctx)
}

func RegisterTemplateDriver(name string, t TemplateDriver) {
	lock.Lock()
	templateDrivers[name] = t
	lock.Unlock()

	if err := t.Prepare(); err != nil {
		panic(err)
	}
}

func FindTemplateSystem(ctx context.Context, name string) (*TemplateSystem, error) {
	lock.RLock()
	t, ok := templateDrivers[name]
	lock.RUnlock()
	if !ok {
		return nil, fmt.Errorf("Template %s not found", name)
	}

	err := t.Init(ctx)
	return &TemplateSystem{t}, err
}

func GetTemplates() []string {
	lock.RLock()
	defer lock.RUnlock()
	t := make([]string, len(templateDrivers))
	var i int
	for k, _ := range templateDrivers {
		t[i] = k
		i++
	}
	return t
}

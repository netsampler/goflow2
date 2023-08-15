package templates

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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

type TemplateKey struct {
	TemplateKey string
	Version     uint16
	ObsDomainId uint32
	TemplateId  uint16
}

func NewTemplateKey(templateKey string, version uint16, obsDomainId uint32, templateId uint16) *TemplateKey {
	return &TemplateKey{
		TemplateKey: templateKey,
		Version:     version,
		ObsDomainId: obsDomainId,
		TemplateId:  templateId,
	}
}

func (k *TemplateKey) String() string {
	return fmt.Sprintf("%s-%d-%d-%d", k.TemplateKey, k.Version, k.ObsDomainId, k.TemplateId)
}

func ParseTemplateKey(key string, k *TemplateKey) error {
	if k != nil {
		return nil
	}
	var version uint16
	var obsDomainId uint32
	var templateId uint16

	keySplit := strings.Split(key, "-")
	if len(keySplit) != 4 {
		return fmt.Errorf("template key format is invalid")
	}
	templateKey := keySplit[0]
	if val, err := strconv.ParseUint(keySplit[1], 10, 64); err != nil {
		return fmt.Errorf("template key version is invalid")
	} else {
		version = uint16(val)
	}
	if val, err := strconv.ParseUint(keySplit[2], 10, 64); err != nil {
		fmt.Errorf("template key observation domain I Dis invalid")
	} else {
		obsDomainId = uint32(val)
	}
	if val, err := strconv.ParseUint(keySplit[3], 10, 64); err != nil {
		fmt.Errorf("template key template ID is invalid")
	} else {
		templateId = uint16(val)
	}

	k.TemplateKey = templateKey
	k.Version = version
	k.ObsDomainId = obsDomainId
	k.TemplateId = templateId

	return nil
}

type TemplateInterface interface {
	ListTemplates(ctx context.Context, ch chan *TemplateKey) error
	GetTemplate(ctx context.Context, key *TemplateKey) (interface{}, error)
	AddTemplate(ctx context.Context, key *TemplateKey, template interface{}) error // add expiration
}

type TemplateSystem struct {
	driver TemplateDriver
}

func (t *TemplateSystem) ListTemplates(ctx context.Context, ch chan *TemplateKey) error {
	return t.driver.ListTemplates(ctx, ch)
}

func (t *TemplateSystem) AddTemplate(ctx context.Context, key *TemplateKey, template interface{}) error {
	return t.driver.AddTemplate(ctx, key, template)
}

func (t *TemplateSystem) GetTemplate(ctx context.Context, key *TemplateKey) (interface{}, error) {
	return t.driver.GetTemplate(ctx, key)
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

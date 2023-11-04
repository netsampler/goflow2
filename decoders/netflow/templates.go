package netflow

import (
	"fmt"
	"sync"
)

var (
	ErrorTemplateNotFound = fmt.Errorf("Error template not found")
)

type FlowBaseTemplateSet map[uint64]interface{}

func templateKey(version uint16, obsDomainId uint32, templateId uint16) uint64 {
	return (uint64(version) << 48) | (uint64(obsDomainId) << 16) | uint64(templateId)
}

// Store interface that allows storing, removing and retrieving template data
type NetFlowTemplateSystem interface {
	RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error)
	GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error)
	AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) error
}

func (ts *BasicTemplateSystem) GetTemplates() FlowBaseTemplateSet {
	ts.templateslock.RLock()
	tmp := ts.templates
	ts.templateslock.RUnlock()
	return tmp
}

func (ts *BasicTemplateSystem) AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) error {
	ts.templateslock.Lock()
	defer ts.templateslock.Unlock()

	/*var templateId uint16
	switch templateIdConv := template.(type) {
	case IPFIXOptionsTemplateRecord:
		templateId = templateIdConv.TemplateId
	case NFv9OptionsTemplateRecord:
		templateId = templateIdConv.TemplateId
	case TemplateRecord:
		templateId = templateIdConv.TemplateId
	}*/
	key := templateKey(version, obsDomainId, templateId)
	ts.templates[key] = template
	return nil
}

func (ts *BasicTemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	ts.templateslock.RLock()
	defer ts.templateslock.RUnlock()
	key := templateKey(version, obsDomainId, templateId)
	if template, ok := ts.templates[key]; ok {
		return template, nil
	}
	return nil, ErrorTemplateNotFound
}

func (ts *BasicTemplateSystem) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	ts.templateslock.Lock()
	defer ts.templateslock.Unlock()

	key := templateKey(version, obsDomainId, templateId)
	if template, ok := ts.templates[key]; ok {
		delete(ts.templates, key)
		return template, nil
	}
	return nil, ErrorTemplateNotFound
}

type BasicTemplateSystem struct {
	templates     FlowBaseTemplateSet
	templateslock *sync.RWMutex
}

// Creates a basic store for NetFlow and IPFIX templates.
// Everyting is stored in memory.
func CreateTemplateSystem() NetFlowTemplateSystem {
	ts := &BasicTemplateSystem{
		templates:     make(FlowBaseTemplateSet),
		templateslock: &sync.RWMutex{},
	}
	return ts
}

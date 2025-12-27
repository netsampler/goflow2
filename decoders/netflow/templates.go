package netflow

import (
	"fmt"
	"maps"
	"sync"
)

var (
	// ErrorTemplateNotFound is returned when a template lookup fails.
	ErrorTemplateNotFound = fmt.Errorf("Error template not found")
)

// FlowBaseTemplateSet is a map keyed by version/obs-domain/template ID.
type FlowBaseTemplateSet map[uint64]interface{}

func templateKey(version uint16, obsDomainId uint32, templateId uint16) uint64 {
	return (uint64(version) << 48) | (uint64(obsDomainId) << 16) | uint64(templateId)
}

// NetFlowTemplateSystem stores NetFlow and IPFIX templates.
type NetFlowTemplateSystem interface {
	RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error)
	GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error)
	AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) error
	GetTemplates() FlowBaseTemplateSet
}

func (ts *BasicTemplateSystem) GetTemplates() FlowBaseTemplateSet {
	ts.templateslock.RLock()
	tmp := make(FlowBaseTemplateSet)
	maps.Copy(tmp, ts.templates)
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

// BasicTemplateSystem keeps templates in-memory with a RW lock.
type BasicTemplateSystem struct {
	templates     FlowBaseTemplateSet
	templateslock *sync.RWMutex
}

// CreateTemplateSystem creates an in-memory store for NetFlow and IPFIX templates.
func CreateTemplateSystem() NetFlowTemplateSystem {
	ts := &BasicTemplateSystem{
		templates:     make(FlowBaseTemplateSet),
		templateslock: &sync.RWMutex{},
	}
	return ts
}

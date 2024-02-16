package netflow

import (
	"fmt"
)

var (
	ErrorTemplateNotFound = fmt.Errorf("Error template not found")
)

// Store interface that allows storing, removing and retrieving template data
type NetFlowTemplateSystem interface {
	RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error)
	GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error)
	AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) error
}

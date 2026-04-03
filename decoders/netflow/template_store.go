package netflow

// FlowContext carries routing metadata for template storage.
type FlowContext struct {
	RouterKey string
}

// TemplateStore stores NetFlow/IPFIX templates keyed by router/version/obs-domain/template ID.
// This is the minimal decoder-facing interface.
type TemplateStore interface {
	AddTemplate(ctx FlowContext, version uint16, obsDomainId uint32, templateId uint16, template interface{}) (TemplateStatus, error)
	GetTemplate(ctx FlowContext, version uint16, obsDomainId uint32, templateId uint16) (interface{}, error)
}

// ManagedTemplateStore adds lifecycle and operational methods on top of TemplateStore.
type ManagedTemplateStore interface {
	TemplateStore
	RemoveTemplate(ctx FlowContext, version uint16, obsDomainId uint32, templateId uint16) (interface{}, bool, error)
	GetAll() map[string]FlowBaseTemplateSet
	Start()
	Close()
}

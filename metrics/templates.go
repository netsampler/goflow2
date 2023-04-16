package metrics

import (
	"strconv"

	"github.com/netsampler/goflow2/v2/decoders/netflow"

	"github.com/prometheus/client_golang/prometheus"
)

type PromTemplateSystem struct {
	key     string
	wrapped netflow.NetFlowTemplateSystem
}

func NewDefaultPromTemplateSystem() netflow.NetFlowTemplateSystem {
	return NewPromTemplateSystem(netflow.CreateTemplateSystem())
}

// todo: improve naming
func NewPromTemplateWrapper(wrapped netflow.NetFlowTemplateSystem) func() netflow.NetFlowTemplateSystem {
	return func() netflow.NetFlowTemplateSystem {
		return NewPromTemplateSystem(wrapped)
	}
}

func NewPromTemplateSystem(wrapped netflow.NetFlowTemplateSystem) netflow.NetFlowTemplateSystem {
	return &PromTemplateSystem{
		key:     "test",
		wrapped: wrapped,
	}
}

func (s *PromTemplateSystem) AddTemplate(version uint16, obsDomainId uint32, template interface{}) error {
	err := s.wrapped.AddTemplate(version, obsDomainId, template)

	typeStr := "options_template"
	var templateId uint16
	switch templateIdConv := template.(type) {
	case netflow.IPFIXOptionsTemplateRecord:
		templateId = templateIdConv.TemplateId
	case netflow.NFv9OptionsTemplateRecord:
		templateId = templateIdConv.TemplateId
	case netflow.TemplateRecord:
		templateId = templateIdConv.TemplateId
		typeStr = "template"
	}
	NetFlowTemplatesStats.With(
		prometheus.Labels{
			"router":        s.key,
			"version":       strconv.Itoa(int(version)),
			"obs_domain_id": strconv.Itoa(int(obsDomainId)),
			"template_id":   strconv.Itoa(int(templateId)),
			"type":          typeStr,
		}).
		Inc()
	return err
}

func (s *PromTemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	return s.wrapped.GetTemplate(version, obsDomainId, templateId)
}

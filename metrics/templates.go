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

// A default Prometheus template generator function to be used by a pipe
func NewDefaultPromTemplateSystem(key string) netflow.NetFlowTemplateSystem {
	return NewPromTemplateSystem(key, netflow.CreateTemplateSystem())
}

// Creates a Prometheus template system that wraps another template system.
// The key argument is providing the router information for metrics.
func NewPromTemplateSystem(key string, wrapped netflow.NetFlowTemplateSystem) netflow.NetFlowTemplateSystem {
	return &PromTemplateSystem{
		key:     key,
		wrapped: wrapped,
	}
}

func (s *PromTemplateSystem) getLabels(version uint16, obsDomainId uint32, template interface{}) prometheus.Labels {

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

	return prometheus.Labels{
		"router":        s.key,
		"version":       strconv.Itoa(int(version)),
		"obs_domain_id": strconv.Itoa(int(obsDomainId)),
		"template_id":   strconv.Itoa(int(templateId)),
		"type":          typeStr,
	}
}

func (s *PromTemplateSystem) AddTemplate(version uint16, obsDomainId uint32, template interface{}) error {
	err := s.wrapped.AddTemplate(version, obsDomainId, template)

	labels := s.getLabels(version, obsDomainId, template)
	NetFlowTemplatesStats.With(
		labels).
		Inc()
	return err
}

func (s *PromTemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	return s.wrapped.GetTemplate(version, obsDomainId, templateId)
}

func (s *PromTemplateSystem) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {

	template, err := s.wrapped.RemoveTemplate(version, obsDomainId, templateId)

	if err == nil {
		labels := s.getLabels(version, obsDomainId, template)

		NetFlowTemplatesStats.Delete(labels)
	}

	return template, err
}

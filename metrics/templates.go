package metrics

import (
	"fmt"
	"strconv"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"

	"github.com/prometheus/client_golang/prometheus"
)

// PromTemplateSystem wraps a template system to record metrics.
type PromTemplateSystem struct {
	key     string
	wrapped netflow.NetFlowTemplateSystem
}

// NewDefaultPromTemplateSystem creates a PromTemplateSystem with default storage.
func NewDefaultPromTemplateSystem(key string) netflow.NetFlowTemplateSystem {
	return NewPromTemplateSystem(key, netflow.CreateTemplateSystem())
}

// NewPromTemplateSystem wraps a template system and records template metrics.
func NewPromTemplateSystem(key string, wrapped netflow.NetFlowTemplateSystem) netflow.NetFlowTemplateSystem {
	return &PromTemplateSystem{
		key:     key,
		wrapped: wrapped,
	}
}

func (s *PromTemplateSystem) getLabels(version uint16, obsDomainId uint32, templateId uint16, template interface{}) prometheus.Labels {

	typeStr := "options_template"
	switch template.(type) {
	case netflow.TemplateRecord:
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

func (s *PromTemplateSystem) AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) (netflow.TemplateStatus, error) {
	update, err := s.wrapped.AddTemplate(version, obsDomainId, templateId, template)
	if err == nil {
		labels := s.getLabels(version, obsDomainId, templateId, template)
		NetFlowTemplatesStats.With(
			labels).
			Inc()
		timestamp := float64(time.Now().Unix())
		switch update {
		case netflow.TemplateAdded:
			NetFlowTemplateAddedTimestamp.With(labels).Set(timestamp)
		case netflow.TemplateUpdated:
			NetFlowTemplateUpdatedTimestamp.With(labels).Set(timestamp)
		}
	}
	if err != nil {
		return update, fmt.Errorf("metrics templates add %d/%d/%d: %w", version, obsDomainId, templateId, err)
	}
	return update, nil
}

// GetTemplate retrieves a template from the wrapped system.
func (s *PromTemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	template, err := s.wrapped.GetTemplate(version, obsDomainId, templateId)
	if err != nil {
		return template, fmt.Errorf("metrics templates get %d/%d/%d: %w", version, obsDomainId, templateId, err)
	}
	labels := s.getLabels(version, obsDomainId, templateId, template)
	NetFlowTemplateAccessedTimestamp.With(labels).Set(float64(time.Now().Unix()))
	return template, nil
}

// RemoveTemplate removes a template and updates metrics.
func (s *PromTemplateSystem) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, bool, error) {
	template, removed, err := s.wrapped.RemoveTemplate(version, obsDomainId, templateId)
	if removed {
		labels := s.getLabels(version, obsDomainId, templateId, template)

		NetFlowTemplatesStats.Delete(labels)
		NetFlowTemplateAddedTimestamp.Delete(labels)
		NetFlowTemplateUpdatedTimestamp.Delete(labels)
		NetFlowTemplateAccessedTimestamp.Delete(labels)
	}

	if err != nil {
		return template, removed, fmt.Errorf("metrics templates remove %d/%d/%d: %w", version, obsDomainId, templateId, err)
	}
	return template, removed, nil
}

// GetTemplates returns all templates from the wrapped system.
func (s *PromTemplateSystem) GetTemplates() netflow.FlowBaseTemplateSet {
	return s.wrapped.GetTemplates()
}

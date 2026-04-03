package metrics

import (
	"strconv"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/utils/store/samplingrate"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
)

// TemplateStoreHooks returns Prometheus hooks for template store lifecycle events.
func TemplateStoreHooks() templates.TemplateHooks {
	series := NewStoreEntryMetrics(
		NetFlowTemplateEntries,
		nil,
		NetFlowTemplateAccessedTimestamp,
	)
	return templates.TemplateHooks{
		OnAdd: func(router string, version uint16, obsDomainId uint32, templateId uint16, template interface{}, updated bool) {
			labels := templateLabels(router, version, obsDomainId, templateId, template)
			NetFlowTemplatesStats.With(labels).Inc()
			timestamp := float64(time.Now().Unix())
			if updated {
				NetFlowTemplateUpdatedTimestamp.With(labels).Set(timestamp)
			} else {
				NetFlowTemplateAddedTimestamp.With(labels).Set(timestamp)
			}
			series.OnWrite(labels)
		},
		OnAccess: func(router string, version uint16, obsDomainId uint32, templateId uint16, template interface{}) {
			series.OnAccess(templateLabels(router, version, obsDomainId, templateId, template))
		},
		OnRemove: func(router string, version uint16, obsDomainId uint32, templateId uint16, template interface{}) {
			labels := templateLabels(router, version, obsDomainId, templateId, template)
			series.OnDelete(labels)
			NetFlowTemplatesStats.Delete(labels)
			NetFlowTemplateAddedTimestamp.Delete(labels)
			NetFlowTemplateUpdatedTimestamp.Delete(labels)
		},
	}
}

// SamplingRateStoreHooks returns Prometheus hooks for sampling-rate store lifecycle events.
func SamplingRateStoreHooks() samplingrate.Hooks {
	series := NewStoreEntryMetrics(
		SamplingRateEntries,
		SamplingRateUpdatedTimestamp,
		SamplingRateAccessedTimestamp,
	)
	return samplingrate.Hooks{
		OnSet: func(router string, version uint16, obsDomainId uint32, rate uint32, existed bool) {
			series.OnWrite(samplingRateLabels(router, version, obsDomainId))
		},
		OnAccess: func(router string, version uint16, obsDomainId uint32, rate uint32) {
			series.OnAccess(samplingRateLabels(router, version, obsDomainId))
		},
		OnRemove: func(router string, version uint16, obsDomainId uint32, rate uint32) {
			series.OnDelete(samplingRateLabels(router, version, obsDomainId))
		},
	}
}

func samplingRateLabels(router string, version uint16, obsDomainId uint32) map[string]string {
	return map[string]string{
		"router":        router,
		"version":       strconv.Itoa(int(version)),
		"obs_domain_id": strconv.Itoa(int(obsDomainId)),
	}
}

func templateLabels(router string, version uint16, obsDomainId uint32, templateId uint16, template interface{}) map[string]string {
	typeStr := "options_template"
	switch template.(type) {
	case netflow.TemplateRecord:
		typeStr = "template"
	}
	return map[string]string{
		"router":        router,
		"version":       strconv.Itoa(int(version)),
		"obs_domain_id": strconv.Itoa(int(obsDomainId)),
		"template_id":   strconv.Itoa(int(templateId)),
		"type":          typeStr,
	}
}

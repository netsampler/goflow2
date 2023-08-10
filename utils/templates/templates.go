package templates

import (
	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

// Function that Create Template Systems.
// This is meant to be used by a pipe
type TemplateSystemGenerator func(key string) netflow.NetFlowTemplateSystem

// Default template generator
func DefaultTemplateGenerator(key string) netflow.NetFlowTemplateSystem {
	return netflow.CreateTemplateSystem()
}

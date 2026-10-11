package netflow

import "errors"

// FlowBaseTemplateSet is a map keyed by version/obs-domain/template ID.
type FlowBaseTemplateSet map[uint64]interface{}

// TemplateStatus describes how a template was stored.
type TemplateStatus uint8

const (
	TemplateUnchanged TemplateStatus = iota
	TemplateUpdated
	TemplateAdded
)

// ErrorTemplateNotFound is returned when a template lookup fails.
var ErrorTemplateNotFound = errors.New("template not found")

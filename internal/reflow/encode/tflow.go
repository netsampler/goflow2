package encode

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

type IPFIXEncoder struct {
	seq             atomic.Uint32
	cfg             config.EncoderConfig
	dataSchemas     map[string]templatedSchemaState
	fallbackPlans   map[string]fallbackTemplatePlan
	sourceOptions   map[string]sourceOptionsState
	lastTemplateRun time.Time
	lastOptionsRun  time.Time
}

type NFv9Encoder struct {
	seq             atomic.Uint32
	cfg             config.EncoderConfig
	dataSchemas     map[string]templatedSchemaState
	fallbackPlans   map[string]fallbackTemplatePlan
	sourceOptions   map[string]sourceOptionsState
	lastTemplateRun time.Time
	lastOptionsRun  time.Time
}

type templatedSchemaState struct {
	stream         string
	fieldNames     []string
	baseTemplateID uint16
	ipv4Template   netflow.TemplateRecord
	ipv6Template   netflow.TemplateRecord
	hasIPv6Variant bool
}

type sourceOptionsState struct {
	stream              string
	agentIP             string
	sourceID            uint32
	observationDomainID uint32
	samplingRate        uint32
	samplePool          uint32
	drops               uint32
	inputIf             uint32
	outputIf            uint32
	templateID          uint16
}

type fallbackTemplatePlan struct {
	template netflow.TemplateRecord
	names    []string
	defs     []config.IPFIXFieldDefinition
}

func NewIPFIXEncoder(cfg config.EncoderConfig) *IPFIXEncoder {
	return &IPFIXEncoder{
		cfg:           cfg,
		dataSchemas:   make(map[string]templatedSchemaState),
		fallbackPlans: make(map[string]fallbackTemplatePlan),
		sourceOptions: make(map[string]sourceOptionsState),
	}
}

func (e *IPFIXEncoder) Encode(evt *event.Event) ([][]byte, error) {
	if evt != nil && evt.Kind == "control" {
		return e.handleControl(evt)
	}
	packet, err := e.buildPacket(evt)
	if err != nil {
		return nil, err
	}
	data, err := netflow.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode ipfix packet: %w", err)
	}
	return [][]byte{data}, nil
}

func (e *IPFIXEncoder) Flush() ([][]byte, error) {
	return e.flushControlPackets(time.Now().UTC())
}

func NewNFv9Encoder(cfg config.EncoderConfig) *NFv9Encoder {
	return &NFv9Encoder{
		cfg:           cfg,
		dataSchemas:   make(map[string]templatedSchemaState),
		fallbackPlans: make(map[string]fallbackTemplatePlan),
		sourceOptions: make(map[string]sourceOptionsState),
	}
}

func (e *NFv9Encoder) Encode(evt *event.Event) ([][]byte, error) {
	if evt != nil && evt.Kind == "control" {
		return e.handleControl(evt)
	}
	packet, err := e.buildPacket(evt)
	if err != nil {
		return nil, err
	}
	data, err := netflow.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode netflow v9 packet: %w", err)
	}
	return [][]byte{data}, nil
}

func (e *NFv9Encoder) Flush() ([][]byte, error) {
	return e.flushControlPackets(time.Now().UTC())
}

// buildPacket translates one runtime event into the appropriate IPFIX packet
// flavor: control/template output or a normal data set.
func (e *IPFIXEncoder) buildPacket(evt *event.Event) (*netflow.IPFIXPacket, error) {
	if evt == nil {
		return nil, fmt.Errorf("nil event")
	}
	if evt.Fields == nil {
		return nil, fmt.Errorf("event fields are empty")
	}

	templateID := uint16Field(evt.Fields, "template_id")
	if templateID == 0 {
		templateID = 256
	}
	obsDomainID := e.observationDomainID(evt.Fields)
	exportTime := uint32(evt.ReceivedAt.Unix())
	if evt.ReceivedAt.IsZero() {
		exportTime = uint32(time.Now().Unix())
	}

	switch evt.Payload.(type) {
	case netflow.TemplateRecord:
		record := evt.Payload.(netflow.TemplateRecord)
		packet := &netflow.IPFIXPacket{
			Version:             10,
			ExportTime:          exportTime,
			SequenceNumber:      e.seq.Load(),
			ObservationDomainId: obsDomainID,
			FlowSets: []interface{}{
				netflow.TemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 2},
					Records:       []netflow.TemplateRecord{record},
				},
			},
		}
		return packet, nil
	case *netflow.TemplateRecord:
		record := *evt.Payload.(*netflow.TemplateRecord)
		packet := &netflow.IPFIXPacket{
			Version:             10,
			ExportTime:          exportTime,
			SequenceNumber:      e.seq.Load(),
			ObservationDomainId: obsDomainID,
			FlowSets: []interface{}{
				netflow.TemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 2},
					Records:       []netflow.TemplateRecord{record},
				},
			},
		}
		return packet, nil
	case netflow.IPFIXOptionsTemplateRecord:
		record := evt.Payload.(netflow.IPFIXOptionsTemplateRecord)
		packet := &netflow.IPFIXPacket{
			Version:             10,
			ExportTime:          exportTime,
			SequenceNumber:      e.seq.Load(),
			ObservationDomainId: obsDomainID,
			FlowSets: []interface{}{
				netflow.IPFIXOptionsTemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 3},
					Records:       []netflow.IPFIXOptionsTemplateRecord{record},
				},
			},
		}
		return packet, nil
	case *netflow.IPFIXOptionsTemplateRecord:
		record := *evt.Payload.(*netflow.IPFIXOptionsTemplateRecord)
		packet := &netflow.IPFIXPacket{
			Version:             10,
			ExportTime:          exportTime,
			SequenceNumber:      e.seq.Load(),
			ObservationDomainId: obsDomainID,
			FlowSets: []interface{}{
				netflow.IPFIXOptionsTemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 3},
					Records:       []netflow.IPFIXOptionsTemplateRecord{record},
				},
			},
		}
		return packet, nil
	}

	stream := eventStream(evt, "flow_data")
	if schema, ok := e.dataSchemas[stream]; ok {
		ipv6 := schema.usesIPv6Template(evt.Fields)
		templateRecord := schema.templateForFamily(ipv6)
		dataRecord, err := buildTemplatedValues(e.cfg.TFlowData, evt.Fields, schema.fieldNames, false, ipv6)
		if err != nil {
			return nil, err
		}
		packet := &netflow.IPFIXPacket{
			Version:             10,
			ExportTime:          exportTime,
			SequenceNumber:      e.seq.Load(),
			ObservationDomainId: obsDomainID,
			FlowSets: []interface{}{
				netflow.DataFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: templateRecord.TemplateId},
					Records:       []netflow.DataRecord{dataRecord},
				},
			},
		}
		e.seq.Add(1)
		return packet, nil
	}

	plan, dataRecord, err := e.fallbackDataRecord(evt.Fields, templateID)
	if err != nil {
		return nil, err
	}
	packet := &netflow.IPFIXPacket{
		Version:             10,
		ExportTime:          exportTime,
		SequenceNumber:      e.seq.Load(),
		ObservationDomainId: obsDomainID,
		FlowSets: []interface{}{
			netflow.TemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 2},
				Records:       []netflow.TemplateRecord{plan.template},
			},
			netflow.DataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: plan.template.TemplateId},
				Records:       []netflow.DataRecord{dataRecord},
			},
		},
	}
	e.seq.Add(1)
	return packet, nil
}

func (e *NFv9Encoder) buildPacket(evt *event.Event) (*netflow.NFv9Packet, error) {
	if evt == nil {
		return nil, fmt.Errorf("nil event")
	}
	if evt.Fields == nil {
		return nil, fmt.Errorf("event fields are empty")
	}

	templateID := uint16Field(evt.Fields, "template_id")
	if templateID == 0 {
		templateID = 256
	}
	sourceID := uint32Field(evt.Fields, "source_id")
	exportMS := exportUnixMilliseconds(evt.ReceivedAt, evt.Fields)
	unixSeconds := uint32((exportMS + 999) / 1000)
	sysUptime, _, _ := uptimeWindow(exportMS, int64Field(evt.Fields, "start_time_unix"), int64Field(evt.Fields, "end_time_unix"))

	switch payload := evt.Payload.(type) {
	case netflow.TemplateRecord:
		return &netflow.NFv9Packet{
			Version:        9,
			Count:          1,
			SystemUptime:   sysUptime,
			UnixSeconds:    unixSeconds,
			SequenceNumber: e.seq.Load(),
			SourceId:       sourceID,
			FlowSets: []interface{}{
				netflow.TemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 0},
					Records:       []netflow.TemplateRecord{payload},
				},
			},
		}, nil
	case *netflow.TemplateRecord:
		return &netflow.NFv9Packet{
			Version:        9,
			Count:          1,
			SystemUptime:   sysUptime,
			UnixSeconds:    unixSeconds,
			SequenceNumber: e.seq.Load(),
			SourceId:       sourceID,
			FlowSets: []interface{}{
				netflow.TemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 0},
					Records:       []netflow.TemplateRecord{*payload},
				},
			},
		}, nil
	case netflow.NFv9OptionsTemplateRecord:
		return &netflow.NFv9Packet{
			Version:        9,
			Count:          1,
			SystemUptime:   sysUptime,
			UnixSeconds:    unixSeconds,
			SequenceNumber: e.seq.Load(),
			SourceId:       sourceID,
			FlowSets: []interface{}{
				netflow.NFv9OptionsTemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 1},
					Records:       []netflow.NFv9OptionsTemplateRecord{payload},
				},
			},
		}, nil
	case *netflow.NFv9OptionsTemplateRecord:
		return &netflow.NFv9Packet{
			Version:        9,
			Count:          1,
			SystemUptime:   sysUptime,
			UnixSeconds:    unixSeconds,
			SequenceNumber: e.seq.Load(),
			SourceId:       sourceID,
			FlowSets: []interface{}{
				netflow.NFv9OptionsTemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 1},
					Records:       []netflow.NFv9OptionsTemplateRecord{*payload},
				},
			},
		}, nil
	}

	stream := eventStream(evt, "flow_data")
	if schema, ok := e.dataSchemas[stream]; ok {
		ipv6 := schema.usesIPv6Template(evt.Fields)
		templateRecord := schema.templateForFamily(ipv6)
		dataRecord, err := buildTemplatedValues(e.cfg.TFlowData, evt.Fields, schema.fieldNames, true, ipv6)
		if err != nil {
			return nil, err
		}
		packet := &netflow.NFv9Packet{
			Version:        9,
			Count:          1,
			SystemUptime:   sysUptime,
			UnixSeconds:    unixSeconds,
			SequenceNumber: e.seq.Load(),
			SourceId:       sourceID,
			FlowSets: []interface{}{
				netflow.DataFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: templateRecord.TemplateId},
					Records:       []netflow.DataRecord{dataRecord},
				},
			},
		}
		e.seq.Add(1)
		return packet, nil
	}

	plan, dataRecord, err := e.fallbackDataRecord(evt.Fields, templateID)
	if err != nil {
		return nil, err
	}
	packet := &netflow.NFv9Packet{
		Version:        9,
		Count:          2,
		SystemUptime:   sysUptime,
		UnixSeconds:    unixSeconds,
		SequenceNumber: e.seq.Load(),
		SourceId:       sourceID,
		FlowSets: []interface{}{
			netflow.TemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 0},
				Records:       []netflow.TemplateRecord{plan.template},
			},
			netflow.DataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: plan.template.TemplateId},
				Records:       []netflow.DataRecord{dataRecord},
			},
		},
	}
	e.seq.Add(1)
	return packet, nil
}

// observationDomainID resolves the IPFIX observation domain from event fields or config.
func (e *IPFIXEncoder) observationDomainID(fields map[string]any) uint32 {
	if e.cfg.ObservationDomainID != 0 {
		return e.cfg.ObservationDomainID
	}
	return uint32Field(fields, "observation_domain_id")
}

// observationDomainID resolves the NetFlow v9 source ID from event fields or config.
func (e *NFv9Encoder) observationDomainID(fields map[string]any) uint32 {
	if e.cfg.ObservationDomainID != 0 {
		return e.cfg.ObservationDomainID
	}
	return uint32Field(fields, "observation_domain_id")
}

// handleControl routes control events into encoder-specific schema/source registration.
func (e *IPFIXEncoder) handleControl(evt *event.Event) ([][]byte, error) {
	switch controlType(evt) {
	case "schema":
		return e.registerSchema(evt)
	case "source_init":
		return e.registerSourceInit(evt)
	default:
		return nil, nil
	}
}

// handleControl routes control events into encoder-specific schema/source registration.
func (e *NFv9Encoder) handleControl(evt *event.Event) ([][]byte, error) {
	switch controlType(evt) {
	case "schema":
		return e.registerSchema(evt)
	case "source_init":
		return e.registerSourceInit(evt)
	default:
		return nil, nil
	}
}

// registerSchema stores aggregation schema state and emits any required template packets.
func (e *IPFIXEncoder) registerSchema(evt *event.Event) ([][]byte, error) {
	schema, ok := evt.Payload.(event.AggregationSchema)
	if !ok {
		if ptr, ok := evt.Payload.(*event.AggregationSchema); ok && ptr != nil {
			schema = *ptr
		} else {
			return nil, nil
		}
	}
	state, err := buildSchemaState(e.cfg.TFlowData, schema, false)
	if err != nil {
		return nil, err
	}
	if state.baseTemplateID == 0 {
		state.baseTemplateID = e.cfg.TemplateBaseID
	}
	if state.ipv4Template.TemplateId == 0 || state.baseTemplateID != state.ipv4Template.TemplateId {
		state, err = buildSchemaStateWithBase(e.cfg.TFlowData, schema, false, state.baseTemplateID)
		if err != nil {
			return nil, err
		}
	}
	e.dataSchemas[eventStream(evt, schema.Stream)] = state
	payloads, err := e.encodeSchemaTemplates(state)
	if err != nil {
		return nil, err
	}
	e.lastTemplateRun = time.Now().UTC()
	return payloads, nil
}

// registerSchema stores aggregation schema state and emits any required template packets.
func (e *NFv9Encoder) registerSchema(evt *event.Event) ([][]byte, error) {
	schema, ok := evt.Payload.(event.AggregationSchema)
	if !ok {
		if ptr, ok := evt.Payload.(*event.AggregationSchema); ok && ptr != nil {
			schema = *ptr
		} else {
			return nil, nil
		}
	}
	state, err := buildSchemaState(e.cfg.TFlowData, schema, true)
	if err != nil {
		return nil, err
	}
	if state.baseTemplateID == 0 {
		state.baseTemplateID = e.cfg.TemplateBaseID
	}
	if state.ipv4Template.TemplateId == 0 || state.baseTemplateID != state.ipv4Template.TemplateId {
		state, err = buildSchemaStateWithBase(e.cfg.TFlowData, schema, true, state.baseTemplateID)
		if err != nil {
			return nil, err
		}
	}
	e.dataSchemas[eventStream(evt, schema.Stream)] = state
	payloads, err := e.encodeSchemaTemplates(state)
	if err != nil {
		return nil, err
	}
	e.lastTemplateRun = time.Now().UTC()
	return payloads, nil
}

// registerSourceInit stores source-scoped exporter metadata and may emit options templates/data.
func (e *IPFIXEncoder) registerSourceInit(evt *event.Event) ([][]byte, error) {
	state := sourceOptionsFromEvent(evt)
	if state.stream == "" {
		state.stream = eventStream(evt, "options_data")
	}
	if state.templateID == 0 {
		state.templateID = e.cfg.OptionsTemplateBaseID
	}
	if e.cfg.ObservationDomainID != 0 {
		state.observationDomainID = e.cfg.ObservationDomainID
	}
	e.sourceOptions[state.stream] = state
	payloads, err := e.encodeSourceOptions(state)
	if err != nil {
		return nil, err
	}
	e.lastOptionsRun = time.Now().UTC()
	return payloads, nil
}

// registerSourceInit stores source-scoped exporter metadata and may emit options templates/data.
func (e *NFv9Encoder) registerSourceInit(evt *event.Event) ([][]byte, error) {
	state := sourceOptionsFromEvent(evt)
	if state.stream == "" {
		state.stream = eventStream(evt, "options_data")
	}
	if state.templateID == 0 {
		state.templateID = e.cfg.OptionsTemplateBaseID
	}
	if e.cfg.ObservationDomainID != 0 {
		state.observationDomainID = e.cfg.ObservationDomainID
	}
	e.sourceOptions[state.stream] = state
	payloads, err := e.encodeSourceOptions(state)
	if err != nil {
		return nil, err
	}
	e.lastOptionsRun = time.Now().UTC()
	return payloads, nil
}

// flushControlPackets emits periodic template/options refresh packets when due.
func (e *IPFIXEncoder) flushControlPackets(now time.Time) ([][]byte, error) {
	var payloads [][]byte
	if e.cfg.TemplateRefresh > 0 && (e.lastTemplateRun.IsZero() || now.Sub(e.lastTemplateRun) >= time.Duration(e.cfg.TemplateRefresh)*time.Millisecond) {
		for _, schema := range e.dataSchemas {
			encoded, err := e.encodeSchemaTemplates(schema)
			if err != nil {
				return nil, err
			}
			payloads = append(payloads, encoded...)
		}
		e.lastTemplateRun = now
	}
	if e.cfg.OptionsRefresh > 0 && (e.lastOptionsRun.IsZero() || now.Sub(e.lastOptionsRun) >= time.Duration(e.cfg.OptionsRefresh)*time.Millisecond) {
		for _, state := range e.sourceOptions {
			encoded, err := e.encodeSourceOptions(state)
			if err != nil {
				return nil, err
			}
			payloads = append(payloads, encoded...)
		}
		e.lastOptionsRun = now
	}
	return payloads, nil
}

// flushControlPackets emits periodic template/options refresh packets when due.
func (e *NFv9Encoder) flushControlPackets(now time.Time) ([][]byte, error) {
	var payloads [][]byte
	if e.cfg.TemplateRefresh > 0 && (e.lastTemplateRun.IsZero() || now.Sub(e.lastTemplateRun) >= time.Duration(e.cfg.TemplateRefresh)*time.Millisecond) {
		for _, schema := range e.dataSchemas {
			encoded, err := e.encodeSchemaTemplates(schema)
			if err != nil {
				return nil, err
			}
			payloads = append(payloads, encoded...)
		}
		e.lastTemplateRun = now
	}
	if e.cfg.OptionsRefresh > 0 && (e.lastOptionsRun.IsZero() || now.Sub(e.lastOptionsRun) >= time.Duration(e.cfg.OptionsRefresh)*time.Millisecond) {
		for _, state := range e.sourceOptions {
			encoded, err := e.encodeSourceOptions(state)
			if err != nil {
				return nil, err
			}
			payloads = append(payloads, encoded...)
		}
		e.lastOptionsRun = now
	}
	return payloads, nil
}

// encodeSchemaTemplates serializes the current stream schema into one or more IPFIX template sets.
func (e *IPFIXEncoder) encodeSchemaTemplates(state templatedSchemaState) ([][]byte, error) {
	now := uint32(time.Now().Unix())
	var out [][]byte
	for _, templateRecord := range state.templates() {
		packet := &netflow.IPFIXPacket{
			Version:             10,
			ExportTime:          now,
			SequenceNumber:      e.seq.Load(),
			ObservationDomainId: e.cfg.ObservationDomainID,
			FlowSets: []interface{}{
				netflow.TemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 2},
					Records:       []netflow.TemplateRecord{templateRecord},
				},
			},
		}
		data, err := netflow.EncodeMessage(packet)
		if err != nil {
			return nil, fmt.Errorf("encode ipfix schema template: %w", err)
		}
		out = append(out, data)
	}
	return out, nil
}

// encodeSchemaTemplates serializes the current stream schema into one or more NetFlow v9 template sets.
func (e *NFv9Encoder) encodeSchemaTemplates(state templatedSchemaState) ([][]byte, error) {
	nowMS := time.Now().UnixMilli()
	nowSec := uint32((nowMS + 999) / 1000)
	var out [][]byte
	for _, templateRecord := range state.templates() {
		packet := &netflow.NFv9Packet{
			Version:        9,
			Count:          1,
			SystemUptime:   0,
			UnixSeconds:    nowSec,
			SequenceNumber: e.seq.Load(),
			SourceId:       e.cfg.ObservationDomainID,
			FlowSets: []interface{}{
				netflow.TemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 0},
					Records:       []netflow.TemplateRecord{templateRecord},
				},
			},
		}
		data, err := netflow.EncodeMessage(packet)
		if err != nil {
			return nil, fmt.Errorf("encode netflow v9 schema template: %w", err)
		}
		out = append(out, data)
	}
	return out, nil
}

// encodeSourceOptions serializes source-level exporter metadata as IPFIX options records.
func (e *IPFIXEncoder) encodeSourceOptions(state sourceOptionsState) ([][]byte, error) {
	packet := &netflow.IPFIXPacket{
		Version:             10,
		ExportTime:          uint32(time.Now().Unix()),
		SequenceNumber:      e.seq.Load(),
		ObservationDomainId: state.observationDomainID,
		FlowSets: []interface{}{
			netflow.IPFIXOptionsTemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 3},
				Records: []netflow.IPFIXOptionsTemplateRecord{
					{
						TemplateId:      state.templateID,
						FieldCount:      2,
						ScopeFieldCount: 1,
						Scopes: []netflow.Field{
							{Type: netflow.IPFIX_FIELD_observationDomainId, Length: 4},
						},
						Options: []netflow.Field{
							{Type: netflow.IPFIX_FIELD_samplingInterval, Length: 4},
						},
					},
				},
			},
			netflow.OptionsDataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: state.templateID},
				Records: []netflow.OptionsDataRecord{
					{
						ScopesValues: []netflow.DataField{
							{Type: netflow.IPFIX_FIELD_observationDomainId, Value: encodeU32(state.sourceID)},
						},
						OptionsValues: []netflow.DataField{
							{Type: netflow.IPFIX_FIELD_samplingInterval, Value: encodeU32(state.samplingRate)},
						},
					},
				},
			},
		},
	}
	data, err := netflow.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode ipfix source options: %w", err)
	}
	return [][]byte{data}, nil
}

// encodeSourceOptions serializes source-level exporter metadata as NetFlow v9 options records.
func (e *NFv9Encoder) encodeSourceOptions(state sourceOptionsState) ([][]byte, error) {
	packet := &netflow.NFv9Packet{
		Version:        9,
		Count:          2,
		SystemUptime:   0,
		UnixSeconds:    uint32(time.Now().Unix()),
		SequenceNumber: e.seq.Load(),
		SourceId:       state.sourceID,
		FlowSets: []interface{}{
			netflow.NFv9OptionsTemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 1},
				Records: []netflow.NFv9OptionsTemplateRecord{
					{
						TemplateId:   state.templateID,
						ScopeLength:  4,
						OptionLength: 4,
						Scopes: []netflow.Field{
							{Type: 1, Length: 4},
						},
						Options: []netflow.Field{
							{Type: netflow.NFV9_FIELD_SAMPLING_INTERVAL, Length: 4},
						},
					},
				},
			},
			netflow.OptionsDataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: state.templateID},
				Records: []netflow.OptionsDataRecord{
					{
						ScopesValues: []netflow.DataField{
							{Type: 1, Value: encodeU32(state.sourceID)},
						},
						OptionsValues: []netflow.DataField{
							{Type: netflow.NFV9_FIELD_SAMPLING_INTERVAL, Value: encodeU32(state.samplingRate)},
						},
					},
				},
			},
		},
	}
	data, err := netflow.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode netflow v9 source options: %w", err)
	}
	return [][]byte{data}, nil
}

// buildSchemaState prepares the template state for an aggregated stream, defaulting
// the base template ID when the schema did not set one explicitly.
func buildSchemaState(cfg config.TFlowDataConfig, schema event.AggregationSchema, netflowV9 bool) (templatedSchemaState, error) {
	baseTemplateID := schema.BaseTemplateID
	if baseTemplateID == 0 {
		baseTemplateID = 256
	}
	return buildSchemaStateWithBase(cfg, schema, netflowV9, baseTemplateID)
}

// buildSchemaStateWithBase precomputes IPv4 and optional IPv6 template variants
// for one aggregated stream schema.
func buildSchemaStateWithBase(cfg config.TFlowDataConfig, schema event.AggregationSchema, netflowV9 bool, baseTemplateID uint16) (templatedSchemaState, error) {
	stream := schema.Stream
	if stream == "" {
		stream = "flow_data"
	}
	if baseTemplateID == 0 {
		baseTemplateID = 256
	}
	state := templatedSchemaState{
		stream:         stream,
		fieldNames:     append([]string(nil), schema.FieldNames...),
		baseTemplateID: baseTemplateID,
	}
	ipv4Template, err := buildTemplateRecordFromFields(cfg, state.fieldNames, baseTemplateID, netflowV9, false)
	if err != nil {
		return templatedSchemaState{}, err
	}
	state.ipv4Template = ipv4Template
	if hasAddressField(state.fieldNames) {
		ipv6Template, err := buildTemplateRecordFromFields(cfg, state.fieldNames, baseTemplateID+1, netflowV9, true)
		if err != nil {
			return templatedSchemaState{}, err
		}
		state.ipv6Template = ipv6Template
		state.hasIPv6Variant = true
	}
	return state, nil
}

// templateForFields selects the IPv6 variant only when the current event needs it.
func (s templatedSchemaState) templateForFields(fields map[string]any) netflow.TemplateRecord {
	if s.hasIPv6Variant && eventHasIPv6(fields) {
		return s.ipv6Template
	}
	return s.ipv4Template
}

// usesIPv6Template reports whether the event requires the IPv6 schema variant.
func (s templatedSchemaState) usesIPv6Template(fields map[string]any) bool {
	return s.hasIPv6Variant && eventHasIPv6(fields)
}

// templateForFamily selects the prebuilt template by IP family.
func (s templatedSchemaState) templateForFamily(ipv6 bool) netflow.TemplateRecord {
	if ipv6 && s.hasIPv6Variant {
		return s.ipv6Template
	}
	return s.ipv4Template
}

// templates returns every template record that must be announced for this schema.
func (s templatedSchemaState) templates() []netflow.TemplateRecord {
	if s.hasIPv6Variant {
		return []netflow.TemplateRecord{s.ipv4Template, s.ipv6Template}
	}
	return []netflow.TemplateRecord{s.ipv4Template}
}

// sourceOptionsFromEvent extracts source-level exporter metadata from either the
// event payload or its normalized fields.
func sourceOptionsFromEvent(evt *event.Event) sourceOptionsState {
	state := sourceOptionsState{
		stream:              eventStream(evt, "options_data"),
		agentIP:             stringFieldOrZero(evt.Fields, "agent_ip"),
		sourceID:            uint32Field(evt.Fields, "source_id"),
		observationDomainID: uint32Field(evt.Fields, "observation_domain_id"),
		samplingRate:        uint32Field(evt.Fields, "sampling_rate"),
		samplePool:          uint32Field(evt.Fields, "sample_pool"),
		drops:               uint32Field(evt.Fields, "drops"),
		inputIf:             uint32Field(evt.Fields, "input_if"),
		outputIf:            uint32Field(evt.Fields, "output_if"),
	}
	if payload, ok := evt.Payload.(event.SourceInit); ok {
		if payload.Stream != "" {
			state.stream = payload.Stream
		}
		if payload.AgentIP != "" {
			state.agentIP = payload.AgentIP
		}
		if payload.SourceID != 0 {
			state.sourceID = payload.SourceID
		}
		if payload.ObservationDomainID != 0 {
			state.observationDomainID = payload.ObservationDomainID
		}
		if payload.SamplingRate != 0 {
			state.samplingRate = payload.SamplingRate
		}
		if payload.SamplePool != 0 {
			state.samplePool = payload.SamplePool
		}
		if payload.Drops != 0 {
			state.drops = payload.Drops
		}
		if payload.InputIf != 0 {
			state.inputIf = payload.InputIf
		}
		if payload.OutputIf != 0 {
			state.outputIf = payload.OutputIf
		}
	}
	return state
}

func (e *IPFIXEncoder) fallbackDataRecord(fieldMap map[string]any, templateID uint16) (fallbackTemplatePlan, netflow.DataRecord, error) {
	key := fallbackPlanKey(e.cfg.TFlowData, fieldMap, templateID, false)
	plan, ok := e.fallbackPlans[key]
	if !ok {
		var err error
		plan, err = buildFallbackPlan(e.cfg.TFlowData, fieldMap, templateID, false)
		if err != nil {
			return fallbackTemplatePlan{}, netflow.DataRecord{}, err
		}
		e.fallbackPlans[key] = plan
	}
	record, err := buildFallbackValues(plan, fieldMap)
	if err != nil {
		return fallbackTemplatePlan{}, netflow.DataRecord{}, err
	}
	return plan, record, nil
}

func (e *NFv9Encoder) fallbackDataRecord(fieldMap map[string]any, templateID uint16) (fallbackTemplatePlan, netflow.DataRecord, error) {
	key := fallbackPlanKey(e.cfg.TFlowData, fieldMap, templateID, true)
	plan, ok := e.fallbackPlans[key]
	if !ok {
		var err error
		plan, err = buildFallbackPlan(e.cfg.TFlowData, fieldMap, templateID, true)
		if err != nil {
			return fallbackTemplatePlan{}, netflow.DataRecord{}, err
		}
		e.fallbackPlans[key] = plan
	}
	record, err := buildFallbackValues(plan, fieldMap)
	if err != nil {
		return fallbackTemplatePlan{}, netflow.DataRecord{}, err
	}
	return plan, record, nil
}

func fallbackPlanKey(cfg config.TFlowDataConfig, fieldMap map[string]any, templateID uint16, netflowV9 bool) string {
	names := selectPresentFlowFields(cfg, fieldMap)
	var b strings.Builder
	fmt.Fprintf(&b, "%t|%d", netflowV9, templateID)
	for _, name := range names {
		def := resolvedFieldDefinition(name, cfg.Catalog[name], fieldMap[name])
		fieldType := def.ID
		if netflowV9 {
			fieldType = def.NetFlowV9ID
			if fieldType == 0 {
				fieldType = def.ID
			}
		}
		length := def.Length
		if length == 0 || length == 0xffff {
			if encoded, err := encodeIPFIXValue(def, fieldMap[name]); err == nil {
				length = ipfixFieldLength(def, encoded)
			}
		}
		fmt.Fprintf(&b, "|%s:%d:%d:%d:%d:%t", name, fieldType, length, def.Length, def.PEN, def.EnterpriseScoped)
	}
	return b.String()
}

func buildFallbackPlan(cfg config.TFlowDataConfig, fieldMap map[string]any, templateID uint16, netflowV9 bool) (fallbackTemplatePlan, error) {
	names := selectPresentFlowFields(cfg, fieldMap)
	fields := make([]netflow.Field, 0, len(names))
	defs := make([]config.IPFIXFieldDefinition, 0, len(names))
	keptNames := make([]string, 0, len(names))
	for _, name := range names {
		def := resolvedFieldDefinition(name, cfg.Catalog[name], fieldMap[name])
		fieldType := def.ID
		if netflowV9 {
			fieldType = def.NetFlowV9ID
			if fieldType == 0 {
				fieldType = def.ID
			}
		}
		encoded, err := encodeIPFIXValue(def, fieldMap[name])
		if err != nil {
			return fallbackTemplatePlan{}, fmt.Errorf("encode field %q: %w", name, err)
		}
		fields = append(fields, netflow.Field{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        fieldType,
			Length:      ipfixFieldLength(def, encoded),
			Pen:         def.PEN,
		})
		defs = append(defs, def)
		keptNames = append(keptNames, name)
	}
	if len(fields) == 0 {
		return fallbackTemplatePlan{}, fmt.Errorf("no encodable fields found for ipfix packet")
	}
	return fallbackTemplatePlan{
		template: netflow.TemplateRecord{
			TemplateId: templateID,
			FieldCount: uint16(len(fields)),
			Fields:     fields,
		},
		names: keptNames,
		defs:  defs,
	}, nil
}

func buildFallbackValues(plan fallbackTemplatePlan, fieldMap map[string]any) (netflow.DataRecord, error) {
	values := make([]netflow.DataField, 0, len(plan.names))
	for i, name := range plan.names {
		val, ok := fieldMap[name]
		if !ok {
			continue
		}
		def := plan.defs[i]
		encoded, err := encodeIPFIXValue(def, val)
		if err != nil {
			return netflow.DataRecord{}, fmt.Errorf("encode field %q: %w", name, err)
		}
		values = append(values, netflow.DataField{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        plan.template.Fields[i].Type,
			Pen:         def.PEN,
			Value:       encoded,
		})
	}
	if len(values) == 0 {
		return netflow.DataRecord{}, fmt.Errorf("no encodable values found for templated packet")
	}
	return netflow.DataRecord{Values: values}, nil
}

func selectPresentFlowFields(cfg config.TFlowDataConfig, fieldMap map[string]any) []string {
	if len(cfg.Select) > 0 {
		names := make([]string, 0, len(cfg.Select))
		for _, name := range cfg.Select {
			if _, ok := cfg.Catalog[name]; !ok {
				continue
			}
			if _, ok := fieldMap[name]; ok {
				names = append(names, name)
			}
		}
		return names
	}
	names := make([]string, 0, len(fieldMap))
	for name := range fieldMap {
		if _, ok := cfg.Catalog[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// buildTemplatedDataRecord picks fields from a runtime event and builds both the
// template and one matching data record.
func buildTemplatedDataRecord(cfg config.TFlowDataConfig, fieldMap map[string]any, templateID uint16, netflowV9 bool) (netflow.TemplateRecord, netflow.DataRecord, error) {
	names := selectFlowFields(cfg, fieldMap)
	return buildTemplatedDataRecordWithNames(cfg, fieldMap, names, templateID, netflowV9)
}

// buildTemplatedDataRecordWithNames uses an explicit field order, which matters
// when schema events already fixed the record layout.
func buildTemplatedDataRecordWithNames(cfg config.TFlowDataConfig, fieldMap map[string]any, names []string, templateID uint16, netflowV9 bool) (netflow.TemplateRecord, netflow.DataRecord, error) {
	templateFields := make([]netflow.Field, 0, len(names))
	values := make([]netflow.DataField, 0, len(names))
	for _, name := range names {
		def, ok := cfg.Catalog[name]
		if !ok {
			continue
		}
		val, ok := fieldMap[name]
		if !ok {
			continue
		}
		def = resolvedFieldDefinition(name, def, val)
		encoded, err := encodeIPFIXValue(def, val)
		if err != nil {
			return netflow.TemplateRecord{}, netflow.DataRecord{}, fmt.Errorf("encode field %q: %w", name, err)
		}
		fieldType := def.ID
		if netflowV9 {
			fieldType = def.NetFlowV9ID
			if fieldType == 0 {
				fieldType = def.ID
			}
		}
		templateFields = append(templateFields, netflow.Field{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        fieldType,
			Length:      ipfixFieldLength(def, encoded),
			Pen:         def.PEN,
		})
		values = append(values, netflow.DataField{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        fieldType,
			Pen:         def.PEN,
			Value:       encoded,
		})
	}
	if len(templateFields) == 0 {
		return netflow.TemplateRecord{}, netflow.DataRecord{}, fmt.Errorf("no encodable fields found for ipfix packet")
	}
	return netflow.TemplateRecord{
			TemplateId: templateID,
			FieldCount: uint16(len(templateFields)),
			Fields:     templateFields,
		}, netflow.DataRecord{
			Values: values,
		}, nil
}

// buildTemplatedValues emits one data record using a preannounced template layout,
// filling missing fields with protocol-appropriate zero values.
func buildTemplatedValues(cfg config.TFlowDataConfig, fieldMap map[string]any, names []string, netflowV9 bool, ipv6 bool) (netflow.DataRecord, error) {
	values := make([]netflow.DataField, 0, len(names))
	for _, name := range names {
		def, ok := cfg.Catalog[name]
		if !ok {
			continue
		}
		def = resolvedFieldDefinitionForFamily(name, def, ipv6)

		val, ok := fieldMap[name]
		var encoded []byte
		var err error
		if ok {
			encoded, err = encodeIPFIXValue(def, val)
			if err != nil {
				return netflow.DataRecord{}, fmt.Errorf("encode field %q: %w", name, err)
			}
		} else {
			encoded, err = defaultEncodedValue(def)
			if err != nil {
				return netflow.DataRecord{}, fmt.Errorf("default field %q: %w", name, err)
			}
		}
		if templateFieldLength(def) == 0xffff {
			encoded, err = encodeVariableLengthValue(encoded)
			if err != nil {
				return netflow.DataRecord{}, fmt.Errorf("encode variable-length field %q: %w", name, err)
			}
		}

		fieldType := def.ID
		if netflowV9 {
			fieldType = def.NetFlowV9ID
			if fieldType == 0 {
				fieldType = def.ID
			}
		}
		values = append(values, netflow.DataField{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        fieldType,
			Pen:         def.PEN,
			Value:       encoded,
		})
	}
	if len(values) == 0 {
		return netflow.DataRecord{}, fmt.Errorf("no encodable values found for templated packet")
	}
	return netflow.DataRecord{Values: values}, nil
}

// buildTemplateRecordFromFields creates a protocol template record without any data.
func buildTemplateRecordFromFields(cfg config.TFlowDataConfig, names []string, templateID uint16, netflowV9 bool, ipv6 bool) (netflow.TemplateRecord, error) {
	fields := make([]netflow.Field, 0, len(names))
	for _, name := range names {
		def, ok := cfg.Catalog[name]
		if !ok {
			continue
		}
		def = resolvedFieldDefinitionForFamily(name, def, ipv6)
		fieldType := def.ID
		if netflowV9 {
			fieldType = def.NetFlowV9ID
			if fieldType == 0 {
				fieldType = def.ID
			}
		}
		fields = append(fields, netflow.Field{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        fieldType,
			Length:      templateFieldLength(def),
			Pen:         def.PEN,
		})
	}
	if len(fields) == 0 {
		return netflow.TemplateRecord{}, fmt.Errorf("no encodable fields found for schema template")
	}
	return netflow.TemplateRecord{
		TemplateId: templateID,
		FieldCount: uint16(len(fields)),
		Fields:     fields,
	}, nil
}

// selectFlowFields uses the configured field whitelist when present, otherwise it
// exports all available fields in sorted order for determinism.
func selectFlowFields(cfg config.TFlowDataConfig, fieldMap map[string]any) []string {
	if len(cfg.Select) > 0 {
		return append([]string(nil), cfg.Select...)
	}
	names := make([]string, 0, len(fieldMap))
	for name := range fieldMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// encodeIPFIXValue encodes one canonical field into the wire representation
// expected by IPFIX or NetFlow v9.
func encodeIPFIXValue(def config.IPFIXFieldDefinition, val any) ([]byte, error) {
	switch def.Type {
	case "ipv4Address", "ipv6Address":
		s, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("expected string IP, got %T", val)
		}
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return nil, err
		}
		if def.Type == "ipv4Address" {
			if !addr.Is4() {
				return nil, fmt.Errorf("expected IPv4 address, got %s", s)
			}
			return append([]byte(nil), addr.AsSlice()...), nil
		}
		if !addr.Is6() {
			return nil, fmt.Errorf("expected IPv6 address, got %s", s)
		}
		return append([]byte(nil), addr.AsSlice()...), nil
	case "unsigned8", "unsigned16", "unsigned32", "unsigned64":
		return encodeUnsigned(def.Type, val)
	case "signed8", "signed16", "signed32", "signed64":
		return encodeSigned(def.Type, val)
	case "string":
		switch v := val.(type) {
		case string:
			return []byte(v), nil
		case []byte:
			return append([]byte(nil), v...), nil
		default:
			return nil, fmt.Errorf("expected string/[]byte, got %T", val)
		}
	default:
		switch v := val.(type) {
		case []byte:
			return append([]byte(nil), v...), nil
		case string:
			return []byte(v), nil
		default:
			return encodeUnsigned("unsigned64", val)
		}
	}
}

// defaultEncodedValue provides a zero representation for fields omitted from a
// templated event but still required by the selected template.
func defaultEncodedValue(def config.IPFIXFieldDefinition) ([]byte, error) {
	switch def.Type {
	case "ipv4Address":
		return make([]byte, 4), nil
	case "ipv6Address":
		return make([]byte, 16), nil
	case "unsigned8", "signed8":
		return make([]byte, 1), nil
	case "unsigned16", "signed16":
		return make([]byte, 2), nil
	case "unsigned32", "signed32":
		return make([]byte, 4), nil
	case "unsigned64", "signed64":
		return make([]byte, 8), nil
	case "string":
		if def.Length == 0xffff || def.Length == 0 {
			return []byte{}, nil
		}
		return make([]byte, def.Length), nil
	default:
		if def.Length == 0xffff || def.Length == 0 {
			return []byte{}, nil
		}
		return make([]byte, def.Length), nil
	}
}

// resolvedFieldDefinition upgrades src_addr/dst_addr to their IPv6 definitions
// when the concrete runtime value contains an IPv6 address.
func resolvedFieldDefinition(name string, def config.IPFIXFieldDefinition, val any) config.IPFIXFieldDefinition {
	ipStr, ok := val.(string)
	if !ok {
		return def
	}
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		return def
	}
	switch name {
	case "src_addr":
		if addr.Is6() {
			def.Name = "sourceIPv6Address"
			def.ID = netflow.IPFIX_FIELD_sourceIPv6Address
			def.NetFlowV9ID = netflow.NFV9_FIELD_IPV6_SRC_ADDR
			def.Length = 16
			def.Type = "ipv6Address"
		}
	case "dst_addr":
		if addr.Is6() {
			def.Name = "destinationIPv6Address"
			def.ID = netflow.IPFIX_FIELD_destinationIPv6Address
			def.NetFlowV9ID = netflow.NFV9_FIELD_IPV6_DST_ADDR
			def.Length = 16
			def.Type = "ipv6Address"
		}
	}
	return def
}

// resolvedFieldDefinitionForFamily performs the same promotion as
// resolvedFieldDefinition, but from a preselected IP family.
func resolvedFieldDefinitionForFamily(name string, def config.IPFIXFieldDefinition, ipv6 bool) config.IPFIXFieldDefinition {
	if !ipv6 {
		return def
	}
	switch name {
	case "src_addr":
		def.Name = "sourceIPv6Address"
		def.ID = netflow.IPFIX_FIELD_sourceIPv6Address
		def.NetFlowV9ID = netflow.NFV9_FIELD_IPV6_SRC_ADDR
		def.Length = 16
		def.Type = "ipv6Address"
	case "dst_addr":
		def.Name = "destinationIPv6Address"
		def.ID = netflow.IPFIX_FIELD_destinationIPv6Address
		def.NetFlowV9ID = netflow.NFV9_FIELD_IPV6_DST_ADDR
		def.Length = 16
		def.Type = "ipv6Address"
	}
	return def
}

// hasAddressField reports whether a schema needs dual IPv4/IPv6 template support.
func hasAddressField(names []string) bool {
	for _, name := range names {
		if name == "src_addr" || name == "dst_addr" {
			return true
		}
	}
	return false
}

// eventHasIPv6 checks the common address fields to determine which template family to use.
func eventHasIPv6(fields map[string]any) bool {
	for _, key := range []string{"src_addr", "dst_addr"} {
		ip := stringFieldOrZero(fields, key)
		if ip == "" {
			continue
		}
		addr, err := netip.ParseAddr(ip)
		if err == nil && addr.Is6() {
			return true
		}
	}
	return false
}

// eventStream prefers the explicit event stream, then control stream, then a caller fallback.
func eventStream(evt *event.Event, fallback string) string {
	if evt != nil && evt.Stream != "" {
		return evt.Stream
	}
	if evt != nil && evt.Control != nil && evt.Control.Stream != "" {
		return evt.Control.Stream
	}
	return fallback
}

// controlType safely returns the event control type when present.
func controlType(evt *event.Event) string {
	if evt == nil || evt.Control == nil {
		return ""
	}
	return evt.Control.Type
}

// encodeU32 writes one uint32 in big-endian order.
func encodeU32(v uint32) []byte {
	return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

// ipfixFieldLength honors explicit field lengths and falls back to encoded size
// for variable-length definitions.
func ipfixFieldLength(def config.IPFIXFieldDefinition, encoded []byte) uint16 {
	if def.Length != 0 {
		return def.Length
	}
	if fixed, ok := fixedIPFIXFieldLength(def.Type); ok {
		return fixed
	}
	if variableLengthIPFIXType(def.Type) {
		return 0xffff
	}
	if len(encoded) > 65535 {
		return 0xffff
	}
	if len(encoded) == 0 {
		return 0xffff
	}
	return uint16(len(encoded))
}

func templateFieldLength(def config.IPFIXFieldDefinition) uint16 {
	if def.Length != 0 {
		return def.Length
	}
	if fixed, ok := fixedIPFIXFieldLength(def.Type); ok {
		return fixed
	}
	return 0xffff
}

func fixedIPFIXFieldLength(kind string) (uint16, bool) {
	switch kind {
	case "ipv4Address", "unsigned32", "signed32":
		return 4, true
	case "ipv6Address":
		return 16, true
	case "unsigned8", "signed8":
		return 1, true
	case "unsigned16", "signed16":
		return 2, true
	case "unsigned64", "signed64":
		return 8, true
	default:
		return 0, false
	}
}

func variableLengthIPFIXType(kind string) bool {
	switch kind {
	case "string", "octetArray":
		return true
	default:
		return false
	}
}

func encodeVariableLengthValue(value []byte) ([]byte, error) {
	if len(value) > 65535 {
		return nil, fmt.Errorf("value length %d exceeds 65535", len(value))
	}
	if len(value) < 255 {
		out := make([]byte, 1, len(value)+1)
		out[0] = byte(len(value))
		out = append(out, value...)
		return out, nil
	}
	out := make([]byte, 3, len(value)+3)
	out[0] = 0xff
	out[1] = byte(len(value) >> 8)
	out[2] = byte(len(value))
	out = append(out, value...)
	return out, nil
}

// encodeUnsigned serializes unsigned integer field kinds in big-endian order.
func encodeUnsigned(kind string, val any) ([]byte, error) {
	v := uint64FromAny(val)
	switch kind {
	case "unsigned8":
		return []byte{byte(v)}, nil
	case "unsigned16":
		return []byte{byte(v >> 8), byte(v)}, nil
	case "unsigned32":
		return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}, nil
	default:
		return []byte{
			byte(v >> 56), byte(v >> 48), byte(v >> 40), byte(v >> 32),
			byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
		}, nil
	}
}

// encodeSigned serializes signed integer field kinds in big-endian order.
func encodeSigned(kind string, val any) ([]byte, error) {
	v := int64Field(map[string]any{"v": val}, "v")
	switch kind {
	case "signed8":
		return []byte{byte(v)}, nil
	case "signed16":
		return []byte{byte(v >> 8), byte(v)}, nil
	case "signed32":
		return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}, nil
	default:
		return []byte{
			byte(v >> 56), byte(v >> 48), byte(v >> 40), byte(v >> 32),
			byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
		}, nil
	}
}

package encode

import (
	"encoding/json"
	"fmt"
	"net"
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
	events          []*event.Event
	estimatedBytes  int
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
	stream           string
	fieldNames       []string
	fields           []event.SchemaField
	baseTemplateID   uint16
	ipv4Template     netflow.TemplateRecord
	ipv6Template     netflow.TemplateRecord
	hasIPv6Variant   bool
	addressGroups    []string
	templateVariants map[uint64]netflow.TemplateRecord
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

type ipfixBatchRecord struct {
	key                 string
	exportTime          uint32
	observationDomainID uint32
	templateID          uint16
	template            *netflow.TemplateRecord
	data                netflow.DataRecord
}

type ipfixBatchSet struct {
	key        string
	templateID uint16
	template   *netflow.TemplateRecord
	records    []netflow.DataRecord
}

type templatedEncodingContext struct {
	netflowV9     bool
	firstSwitched uint32
	lastSwitched  uint32
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
		payloads, err := e.flushDataPackets()
		if err != nil {
			return nil, err
		}
		controlPayloads, err := e.handleControl(evt)
		if err != nil {
			return nil, err
		}
		return append(payloads, controlPayloads...), nil
	}
	if !e.cfg.Batch.IsEnabled() {
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
	e.appendEvent(evt)
	if e.shouldFlush() {
		return e.flushDataPackets()
	}
	return nil, nil
}

func (e *IPFIXEncoder) Flush() ([][]byte, error) {
	payloads, err := e.flushDataPackets()
	if err != nil {
		return nil, err
	}
	controlPayloads, err := e.flushControlPackets(time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return append(payloads, controlPayloads...), nil
}

func (e *IPFIXEncoder) appendEvent(evt *event.Event) {
	e.events = append(e.events, evt)
	e.estimatedBytes += estimatedEventSize(evt)
}

func (e *IPFIXEncoder) shouldFlush() bool {
	if len(e.events) == 0 {
		return false
	}
	if e.cfg.Batch.MaxRecords > 0 && len(e.events) >= e.cfg.Batch.MaxRecords {
		return true
	}
	if e.cfg.Batch.MaxBytes > 0 && e.estimatedBytes >= e.cfg.Batch.MaxBytes {
		return true
	}
	return false
}

func (e *IPFIXEncoder) flushDataPackets() ([][]byte, error) {
	if len(e.events) == 0 {
		return nil, nil
	}

	pending := e.events

	var payloads [][]byte
	for len(pending) > 0 {
		packet, accepted, err := e.buildBatchedPacket(pending)
		if err != nil {
			e.events = pending
			e.estimatedBytes = estimatedEventsSize(pending)
			return payloads, err
		}
		data, err := netflow.EncodeMessage(packet)
		if err != nil {
			e.events = pending
			e.estimatedBytes = estimatedEventsSize(pending)
			return payloads, fmt.Errorf("encode ipfix packet: %w", err)
		}
		payloads = append(payloads, data)
		pending = pending[accepted:]
		e.events = pending
		e.estimatedBytes = estimatedEventsSize(pending)
	}
	e.events = nil
	e.estimatedBytes = 0
	return payloads, nil
}

func estimatedEventsSize(events []*event.Event) int {
	var total int
	for _, evt := range events {
		total += estimatedEventSize(evt)
	}
	return total
}

func (e *IPFIXEncoder) buildBatchedPacket(events []*event.Event) (*netflow.IPFIXPacket, int, error) {
	if len(events) == 0 {
		return nil, 0, fmt.Errorf("empty ipfix packet batch")
	}
	first, err := e.ipfixBatchRecord(events[0])
	if err != nil {
		return nil, 0, err
	}
	sets := []ipfixBatchSet{{
		key:        first.key,
		templateID: first.templateID,
		template:   first.template,
		records:    []netflow.DataRecord{first.data},
	}}
	packet := e.ipfixBatchPacket(first, sets)
	accepted := 1

	for _, evt := range events[1:] {
		next, err := e.ipfixBatchRecord(evt)
		if err != nil {
			return nil, accepted, err
		}
		if next.observationDomainID != first.observationDomainID {
			break
		}
		nextSets, ok := appendIPFIXBatchRecord(sets, next)
		if !ok {
			break
		}
		packet = e.ipfixBatchPacket(first, nextSets)
		if e.cfg.MaxDatagramBytes > 0 {
			data, err := netflow.EncodeMessage(packet)
			if err != nil {
				return nil, accepted, fmt.Errorf("encode ipfix packet: %w", err)
			}
			if len(data) > e.cfg.MaxDatagramBytes {
				packet = e.ipfixBatchPacket(first, sets)
				break
			}
		}
		sets = nextSets
		accepted++
	}

	e.advanceIPFIXSequence(packet)
	return packet, accepted, nil
}

func appendIPFIXBatchRecord(sets []ipfixBatchSet, record ipfixBatchRecord) ([]ipfixBatchSet, bool) {
	next := make([]ipfixBatchSet, len(sets))
	copy(next, sets)
	for i := range next {
		if next[i].templateID != record.templateID {
			continue
		}
		if next[i].key != record.key {
			return nil, false
		}
		next[i].records = append(append([]netflow.DataRecord(nil), next[i].records...), record.data)
		return next, true
	}
	next = append(next, ipfixBatchSet{
		key:        record.key,
		templateID: record.templateID,
		template:   record.template,
		records:    []netflow.DataRecord{record.data},
	})
	return next, true
}

func (e *IPFIXEncoder) ipfixBatchPacket(first ipfixBatchRecord, sets []ipfixBatchSet) *netflow.IPFIXPacket {
	flowSets := make([]interface{}, 0, len(sets)+1)
	templates := make([]netflow.TemplateRecord, 0, len(sets))
	seenTemplates := make(map[uint16]struct{}, len(sets))
	for _, set := range sets {
		if set.template == nil {
			continue
		}
		if _, ok := seenTemplates[set.templateID]; ok {
			continue
		}
		seenTemplates[set.templateID] = struct{}{}
		templates = append(templates, *set.template)
	}
	if len(templates) > 0 {
		flowSets = append(flowSets, netflow.TemplateFlowSet{
			FlowSetHeader: netflow.FlowSetHeader{Id: 2},
			Records:       templates,
		})
	}
	for _, set := range sets {
		flowSets = append(flowSets, netflow.DataFlowSet{
			FlowSetHeader: netflow.FlowSetHeader{Id: set.templateID},
			Records:       set.records,
		})
	}
	return &netflow.IPFIXPacket{
		Version:             10,
		ExportTime:          first.exportTime,
		SequenceNumber:      e.seq.Load(),
		ObservationDomainId: first.observationDomainID,
		FlowSets:            flowSets,
	}
}

func (e *IPFIXEncoder) advanceIPFIXSequence(packet *netflow.IPFIXPacket) {
	if packet == nil {
		return
	}
	e.seq.Add(ipfixSequenceIncrement(packet.FlowSets))
}

func ipfixSequenceIncrement(flowSets []interface{}) uint32 {
	var count uint32
	for _, flowSet := range flowSets {
		switch fs := flowSet.(type) {
		case netflow.DataFlowSet:
			count += uint32(len(fs.Records))
		case *netflow.DataFlowSet:
			if fs != nil {
				count += uint32(len(fs.Records))
			}
		case netflow.OptionsDataFlowSet:
			count += uint32(len(fs.Records))
		case *netflow.OptionsDataFlowSet:
			if fs != nil {
				count += uint32(len(fs.Records))
			}
		}
	}
	return count
}

func (e *IPFIXEncoder) ipfixBatchRecord(evt *event.Event) (ipfixBatchRecord, error) {
	if evt == nil {
		return ipfixBatchRecord{}, fmt.Errorf("nil event")
	}
	if evt.Fields == nil {
		return ipfixBatchRecord{}, fmt.Errorf("event fields are empty")
	}
	fieldMap := eventFieldsWithMetadata(evt)
	templateID := uint16Field(fieldMap, "template_id")
	if templateID == 0 {
		templateID = 256
	}
	exportTime := uint32(evt.ReceivedAt.Unix())
	if evt.ReceivedAt.IsZero() {
		exportTime = uint32(time.Now().Unix())
	}
	obsDomainID := e.observationDomainID()

	stream := eventStream(evt, "flow_data")
	if schema, ok := e.dataSchemas[stream]; ok {
		fieldMap = eventFieldsWithMetadataForSchema(evt, schema.fields)
		mask := schema.addressVariantMask(fieldMap)
		templateRecord := schema.templateForMask(mask)
		dataRecord, err := buildTemplatedValuesFromSchemaFieldsForMask(e.cfg.TemplatedFlow.Data, fieldMap, schema.fields, templatedEncodingContext{}, schema.addressGroups, mask)
		if err != nil {
			return ipfixBatchRecord{}, err
		}
		return ipfixBatchRecord{
			key:                 fmt.Sprintf("schema:%s:%d", stream, templateRecord.TemplateId),
			exportTime:          exportTime,
			observationDomainID: obsDomainID,
			templateID:          templateRecord.TemplateId,
			data:                dataRecord,
		}, nil
	}

	plan, dataRecord, err := e.fallbackDataRecord(fieldMap, templateID)
	if err != nil {
		return ipfixBatchRecord{}, err
	}
	key, err := fallbackPlanKey(e.cfg.TemplatedFlow.Data, fieldMap, templateID, false)
	if err != nil {
		return ipfixBatchRecord{}, err
	}
	return ipfixBatchRecord{
		key:                 key,
		exportTime:          exportTime,
		observationDomainID: obsDomainID,
		templateID:          plan.template.TemplateId,
		template:            &plan.template,
		data:                dataRecord,
	}, nil
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

	fieldMap := eventFieldsWithMetadata(evt)
	templateID := uint16Field(fieldMap, "template_id")
	if templateID == 0 {
		templateID = 256
	}
	obsDomainID := e.observationDomainID()
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
		e.advanceIPFIXSequence(packet)
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
		e.advanceIPFIXSequence(packet)
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
		e.advanceIPFIXSequence(packet)
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
		e.advanceIPFIXSequence(packet)
		return packet, nil
	}

	stream := eventStream(evt, "flow_data")
	if schema, ok := e.dataSchemas[stream]; ok {
		fieldMap = eventFieldsWithMetadataForSchema(evt, schema.fields)
		mask := schema.addressVariantMask(fieldMap)
		templateRecord := schema.templateForMask(mask)
		dataRecord, err := buildTemplatedValuesFromSchemaFieldsForMask(e.cfg.TemplatedFlow.Data, fieldMap, schema.fields, templatedEncodingContext{}, schema.addressGroups, mask)
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
		e.advanceIPFIXSequence(packet)
		return packet, nil
	}

	plan, dataRecord, err := e.fallbackDataRecord(fieldMap, templateID)
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
	e.advanceIPFIXSequence(packet)
	return packet, nil
}

func (e *NFv9Encoder) buildPacket(evt *event.Event) (*netflow.NFv9Packet, error) {
	if evt == nil {
		return nil, fmt.Errorf("nil event")
	}
	if evt.Fields == nil {
		return nil, fmt.Errorf("event fields are empty")
	}

	fieldMap := eventFieldsWithMetadata(evt)
	templateID := uint16Field(fieldMap, "template_id")
	if templateID == 0 {
		templateID = 256
	}
	sourceID := e.sourceID()
	exportMS := exportUnixMilliseconds(evt.ReceivedAt, evt.Fields)
	unixSeconds := uint32((exportMS + 999) / 1000)
	sysUptime, firstSwitched, lastSwitched := uptimeWindow(exportMS, int64Field(evt.Fields, "start_time_unix"), int64Field(evt.Fields, "end_time_unix"))
	encodingCtx := templatedEncodingContext{
		netflowV9:     true,
		firstSwitched: firstSwitched,
		lastSwitched:  lastSwitched,
	}

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
		fieldMap = eventFieldsWithMetadataForSchema(evt, schema.fields)
		mask := schema.addressVariantMask(fieldMap)
		templateRecord := schema.templateForMask(mask)
		dataRecord, err := buildTemplatedValuesFromSchemaFieldsForMask(e.cfg.TemplatedFlow.Data, fieldMap, schema.fields, encodingCtx, schema.addressGroups, mask)
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

	plan, dataRecord, err := e.fallbackDataRecord(fieldMap, templateID, encodingCtx)
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

// observationDomainID is exporter-scoped. Per-interface IDs stay in data fields
// such as source_id/observationPointId, not in the IPFIX packet header.
func (e *IPFIXEncoder) observationDomainID() uint32 {
	return e.cfg.TemplatedFlow.ObservationDomainID
}

// sourceID is the NetFlow v9 exporter source ID, equivalent to the configured
// exporter observation domain.
func (e *NFv9Encoder) sourceID() uint32 {
	return e.cfg.TemplatedFlow.ObservationDomainID
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
	state, err := buildSchemaState(e.cfg.TemplatedFlow.Data, schema, false)
	if err != nil {
		return nil, err
	}
	if state.baseTemplateID == 0 {
		state.baseTemplateID = e.cfg.TemplatedFlow.TemplateBaseID
	}
	if state.ipv4Template.TemplateId == 0 || state.baseTemplateID != state.ipv4Template.TemplateId {
		state, err = buildSchemaStateWithBase(e.cfg.TemplatedFlow.Data, schema, false, state.baseTemplateID)
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
	state, err := buildSchemaState(e.cfg.TemplatedFlow.Data, schema, true)
	if err != nil {
		return nil, err
	}
	if state.baseTemplateID == 0 {
		state.baseTemplateID = e.cfg.TemplatedFlow.TemplateBaseID
	}
	if state.ipv4Template.TemplateId == 0 || state.baseTemplateID != state.ipv4Template.TemplateId {
		state, err = buildSchemaStateWithBase(e.cfg.TemplatedFlow.Data, schema, true, state.baseTemplateID)
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
		state.templateID = e.cfg.TemplatedFlow.OptionsTemplateBaseID
	}
	state.observationDomainID = e.observationDomainID()
	e.sourceOptions[sourceOptionsKey(state)] = state
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
		state.templateID = e.cfg.TemplatedFlow.OptionsTemplateBaseID
	}
	state.observationDomainID = e.sourceID()
	e.sourceOptions[sourceOptionsKey(state)] = state
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
	if e.cfg.TemplatedFlow.TemplateRefresh > 0 && (e.lastTemplateRun.IsZero() || now.Sub(e.lastTemplateRun) >= time.Duration(e.cfg.TemplatedFlow.TemplateRefresh)*time.Millisecond) {
		for _, schema := range e.dataSchemas {
			encoded, err := e.encodeSchemaTemplates(schema)
			if err != nil {
				return nil, err
			}
			payloads = append(payloads, encoded...)
		}
		e.lastTemplateRun = now
	}
	if e.cfg.TemplatedFlow.OptionsRefresh > 0 && (e.lastOptionsRun.IsZero() || now.Sub(e.lastOptionsRun) >= time.Duration(e.cfg.TemplatedFlow.OptionsRefresh)*time.Millisecond) {
		encoded, err := e.encodeSourceOptionsBatch(sortedSourceOptions(e.sourceOptions))
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, encoded...)
		e.lastOptionsRun = now
	}
	return payloads, nil
}

// flushControlPackets emits periodic template/options refresh packets when due.
func (e *NFv9Encoder) flushControlPackets(now time.Time) ([][]byte, error) {
	var payloads [][]byte
	if e.cfg.TemplatedFlow.TemplateRefresh > 0 && (e.lastTemplateRun.IsZero() || now.Sub(e.lastTemplateRun) >= time.Duration(e.cfg.TemplatedFlow.TemplateRefresh)*time.Millisecond) {
		for _, schema := range e.dataSchemas {
			encoded, err := e.encodeSchemaTemplates(schema)
			if err != nil {
				return nil, err
			}
			payloads = append(payloads, encoded...)
		}
		e.lastTemplateRun = now
	}
	if e.cfg.TemplatedFlow.OptionsRefresh > 0 && (e.lastOptionsRun.IsZero() || now.Sub(e.lastOptionsRun) >= time.Duration(e.cfg.TemplatedFlow.OptionsRefresh)*time.Millisecond) {
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
			ObservationDomainId: e.cfg.TemplatedFlow.ObservationDomainID,
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
		e.advanceIPFIXSequence(packet)
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
			SourceId:       e.cfg.TemplatedFlow.ObservationDomainID,
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
	return e.encodeSourceOptionsBatch([]sourceOptionsState{state})
}

func (e *IPFIXEncoder) encodeSourceOptionsBatch(states []sourceOptionsState) ([][]byte, error) {
	groups := groupedIPFIXSourceOptions(states)
	out := make([][]byte, 0, len(groups))
	for _, states := range groups {
		data, err := e.encodeSourceOptionsGroup(states)
		if err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return out, nil
}

func (e *IPFIXEncoder) encodeSourceOptionsGroup(states []sourceOptionsState) ([]byte, error) {
	if len(states) == 0 {
		return nil, nil
	}
	first := states[0]
	records := make([]netflow.OptionsDataRecord, 0, len(states))
	for _, state := range states {
		records = append(records, netflow.OptionsDataRecord{
			ScopesValues: []netflow.DataField{
				{Type: netflow.IPFIX_FIELD_observationPointId, Value: encodeU64(uint64(state.sourceID))},
			},
			OptionsValues: []netflow.DataField{
				{Type: netflow.IPFIX_FIELD_samplingInterval, Value: encodeU32(state.samplingRate)},
			},
		})
	}
	packet := &netflow.IPFIXPacket{
		Version:             10,
		ExportTime:          uint32(time.Now().Unix()),
		SequenceNumber:      e.seq.Load(),
		ObservationDomainId: first.observationDomainID,
		FlowSets: []interface{}{
			netflow.IPFIXOptionsTemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 3},
				Records: []netflow.IPFIXOptionsTemplateRecord{
					{
						TemplateId:      first.templateID,
						FieldCount:      2,
						ScopeFieldCount: 1,
						Scopes: []netflow.Field{
							{Type: netflow.IPFIX_FIELD_observationPointId, Length: 8},
						},
						Options: []netflow.Field{
							{Type: netflow.IPFIX_FIELD_samplingInterval, Length: 4},
						},
					},
				},
			},
			netflow.OptionsDataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: first.templateID},
				Records:       records,
			},
		},
	}
	data, err := netflow.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode ipfix source options: %w", err)
	}
	e.advanceIPFIXSequence(packet)
	return data, nil
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
func buildSchemaState(cfg config.TemplatedFlowDataConfig, schema event.AggregationSchema, netflowV9 bool) (templatedSchemaState, error) {
	baseTemplateID := schema.BaseTemplateID
	if baseTemplateID == 0 {
		baseTemplateID = 256
	}
	return buildSchemaStateWithBase(cfg, schema, netflowV9, baseTemplateID)
}

// buildSchemaStateWithBase precomputes IPv4 and optional IPv6 template variants
// for one aggregated stream schema.
func buildSchemaStateWithBase(cfg config.TemplatedFlowDataConfig, schema event.AggregationSchema, netflowV9 bool, baseTemplateID uint16) (templatedSchemaState, error) {
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
		fields:         schemaFieldsOrNames(schema),
		baseTemplateID: baseTemplateID,
	}
	state.addressGroups = schemaAddressGroups(cfg, state.fields)
	if len(state.addressGroups) > 8 {
		return templatedSchemaState{}, fmt.Errorf("schema has %d address groups; maximum is 8", len(state.addressGroups))
	}
	if mask, ok := fixedAddressMask(schema, len(state.addressGroups)); ok {
		template, err := buildTemplateRecordFromSchemaFieldsForMask(cfg, state.fields, baseTemplateID, netflowV9, state.addressGroups, mask)
		if err != nil {
			return templatedSchemaState{}, err
		}
		state.templateVariants = map[uint64]netflow.TemplateRecord{mask: template}
		state.ipv4Template = template
		state.ipv6Template = template
		state.hasIPv6Variant = mask != 0
		return state, nil
	}
	variantCount := uint64(1)
	if len(state.addressGroups) > 0 {
		variantCount = 1 << len(state.addressGroups)
	}
	if uint64(baseTemplateID)+variantCount-1 > 0xffff {
		return templatedSchemaState{}, fmt.Errorf("template id range %d..%d exceeds 65535", baseTemplateID, uint64(baseTemplateID)+variantCount-1)
	}
	variantMasks := orderedAddressVariantMasks(state.addressGroups)
	state.templateVariants = make(map[uint64]netflow.TemplateRecord, variantCount)
	for i, mask := range variantMasks {
		template, err := buildTemplateRecordFromSchemaFieldsForMask(cfg, state.fields, baseTemplateID+uint16(i), netflowV9, state.addressGroups, mask)
		if err != nil {
			return templatedSchemaState{}, err
		}
		state.templateVariants[mask] = template
		if mask == 0 {
			state.ipv4Template = template
		}
		if mask == variantCount-1 {
			state.ipv6Template = template
		}
	}
	state.hasIPv6Variant = variantCount > 1
	return state, nil
}

func fixedAddressMask(schema event.AggregationSchema, groupCount int) (uint64, bool) {
	if groupCount == 0 || len(schema.Match) == 0 {
		return 0, false
	}
	switch strings.ToLower(schema.Match["ip_family"]) {
	case "ipv4", "4":
		return 0, true
	case "ipv6", "6":
		return (uint64(1) << groupCount) - 1, true
	default:
		return 0, false
	}
}

func schemaFieldsOrNames(schema event.AggregationSchema) []event.SchemaField {
	if len(schema.Fields) > 0 {
		return append([]event.SchemaField(nil), schema.Fields...)
	}
	fields := make([]event.SchemaField, 0, len(schema.FieldNames))
	for _, name := range schema.FieldNames {
		fields = append(fields, event.SchemaField{Name: name, Role: "current"})
	}
	return fields
}

func schemaNeedsIPv6Variant(fields []event.SchemaField) bool {
	for _, field := range fields {
		if isSourceAddressField(field.Name) || isDestinationAddressField(field.Name) {
			return true
		}
	}
	return false
}

// templateForFields selects the address-family variant needed by the current event.
func (s templatedSchemaState) templateForFields(fields map[string]any) netflow.TemplateRecord {
	return s.templateForMask(s.addressVariantMask(fields))
}

// usesIPv6Template reports whether the event requires any IPv6 schema variant.
func (s templatedSchemaState) usesIPv6Template(fields map[string]any) bool {
	return s.addressVariantMask(fields) != 0
}

// templateForFamily selects the prebuilt template by IP family.
func (s templatedSchemaState) templateForFamily(ipv6 bool) netflow.TemplateRecord {
	if !ipv6 {
		return s.templateForMask(0)
	}
	if len(s.addressGroups) == 0 {
		return s.ipv4Template
	}
	return s.templateForMask((uint64(1) << len(s.addressGroups)) - 1)
}

func (s templatedSchemaState) templateForMask(mask uint64) netflow.TemplateRecord {
	if len(s.templateVariants) == 0 {
		if mask != 0 && s.hasIPv6Variant {
			return s.ipv6Template
		}
		return s.ipv4Template
	}
	if template, ok := s.templateVariants[mask]; ok {
		return template
	}
	return s.templateVariants[0]
}

// templates returns every template record that must be announced for this schema.
func (s templatedSchemaState) templates() []netflow.TemplateRecord {
	if len(s.templateVariants) == 0 {
		if s.hasIPv6Variant {
			return []netflow.TemplateRecord{s.ipv4Template, s.ipv6Template}
		}
		return []netflow.TemplateRecord{s.ipv4Template}
	}
	masks := make([]uint64, 0, len(s.templateVariants))
	for mask := range s.templateVariants {
		masks = append(masks, mask)
	}
	sort.Slice(masks, func(i, j int) bool {
		return s.templateVariants[masks[i]].TemplateId < s.templateVariants[masks[j]].TemplateId
	})
	templates := make([]netflow.TemplateRecord, 0, len(masks))
	for _, mask := range masks {
		templates = append(templates, s.templateVariants[mask])
	}
	return templates
}

func (s templatedSchemaState) addressVariantMask(fields map[string]any) uint64 {
	if len(s.addressGroups) == 0 || len(fields) == 0 {
		return 0
	}
	groupIndexes := make(map[string]int, len(s.addressGroups))
	for i, group := range s.addressGroups {
		groupIndexes[group] = i
	}
	var mask uint64
	for _, field := range s.fields {
		group, ok := addressFieldGroup(field.Name)
		if !ok {
			continue
		}
		index, ok := groupIndexes[group]
		if !ok {
			continue
		}
		if fieldValueIsIPv6(fields[field.Name]) {
			mask |= 1 << index
		}
	}
	return mask
}

func orderedAddressVariantMasks(groups []string) []uint64 {
	if len(groups) == 0 {
		return []uint64{0}
	}
	variantCount := uint64(1) << len(groups)
	out := make([]uint64, 0, variantCount)
	seen := make(map[uint64]bool, variantCount)
	appendMask := func(mask uint64) {
		if seen[mask] {
			return
		}
		seen[mask] = true
		out = append(out, mask)
	}

	appendMask(0)
	if mask, ok := preferredNATVariantMask(groups); ok {
		appendMask(mask)
	}
	// Mixed original/NAT address-family variants follow the paired IPv4 and IPv6
	// templates. With the generated NAT schema, those become template 258 for
	// NAT64 and template 259 for NAT46.
	for mask := uint64(1); mask < variantCount; mask++ {
		appendMask(mask)
	}
	return out
}

func preferredNATVariantMask(groups []string) (uint64, bool) {
	primaryIndex := -1
	natIndex := -1
	for i, group := range groups {
		switch group {
		case "":
			primaryIndex = i
		case "nat":
			natIndex = i
		}
	}
	if primaryIndex < 0 || natIndex < 0 {
		return 0, false
	}
	return (uint64(1) << primaryIndex) | (uint64(1) << natIndex), true
}

func sourceOptionsKey(state sourceOptionsState) string {
	return fmt.Sprintf("%s|%d|%d|%d", state.stream, state.observationDomainID, state.sourceID, state.templateID)
}

func sortedSourceOptions(options map[string]sourceOptionsState) []sourceOptionsState {
	if len(options) == 0 {
		return nil
	}
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	states := make([]sourceOptionsState, 0, len(keys))
	for _, key := range keys {
		states = append(states, options[key])
	}
	return states
}

func groupedIPFIXSourceOptions(states []sourceOptionsState) [][]sourceOptionsState {
	if len(states) == 0 {
		return nil
	}
	groups := make([][]sourceOptionsState, 0, len(states))
	groupIndexes := make(map[string]int, len(states))
	for _, state := range states {
		key := fmt.Sprintf("%d|%d", state.observationDomainID, state.templateID)
		if index, ok := groupIndexes[key]; ok {
			groups[index] = append(groups[index], state)
			continue
		}
		groupIndexes[key] = len(groups)
		groups = append(groups, []sourceOptionsState{state})
	}
	return groups
}

// sourceOptionsFromEvent extracts source-level exporter metadata from event
// metadata, with payload/field values used only for exporter control knobs that
// are not represented in SourceMetadata.
func sourceOptionsFromEvent(evt *event.Event) sourceOptionsState {
	state := sourceOptionsState{
		stream:       eventStream(evt, "options_data"),
		agentIP:      eventAgentIP(evt),
		sourceID:     eventSourceID(evt),
		samplingRate: eventSamplingRate(evt),
		samplePool:   eventSamplePool(evt),
		drops:        eventDrops(evt),
		inputIf:      uint32Field(evt.Fields, "input_if"),
		outputIf:     uint32Field(evt.Fields, "output_if"),
	}
	if payload, ok := evt.Payload.(event.SourceInit); ok {
		if payload.Stream != "" {
			state.stream = payload.Stream
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
	key, err := fallbackPlanKey(e.cfg.TemplatedFlow.Data, fieldMap, templateID, false)
	if err != nil {
		return fallbackTemplatePlan{}, netflow.DataRecord{}, err
	}
	plan, ok := e.fallbackPlans[key]
	if !ok {
		plan, err = buildFallbackPlan(e.cfg.TemplatedFlow.Data, fieldMap, templateID, false)
		if err != nil {
			return fallbackTemplatePlan{}, netflow.DataRecord{}, err
		}
		e.fallbackPlans[key] = plan
	}
	record, err := buildFallbackValues(plan, fieldMap, templatedEncodingContext{})
	if err != nil {
		return fallbackTemplatePlan{}, netflow.DataRecord{}, err
	}
	return plan, record, nil
}

func (e *NFv9Encoder) fallbackDataRecord(fieldMap map[string]any, templateID uint16, encodingCtx templatedEncodingContext) (fallbackTemplatePlan, netflow.DataRecord, error) {
	key, err := fallbackPlanKey(e.cfg.TemplatedFlow.Data, fieldMap, templateID, true)
	if err != nil {
		return fallbackTemplatePlan{}, netflow.DataRecord{}, err
	}
	plan, ok := e.fallbackPlans[key]
	if !ok {
		plan, err = buildFallbackPlan(e.cfg.TemplatedFlow.Data, fieldMap, templateID, true)
		if err != nil {
			return fallbackTemplatePlan{}, netflow.DataRecord{}, err
		}
		e.fallbackPlans[key] = plan
	}
	record, err := buildFallbackValues(plan, fieldMap, encodingCtx)
	if err != nil {
		return fallbackTemplatePlan{}, netflow.DataRecord{}, err
	}
	return plan, record, nil
}

func fallbackPlanKey(cfg config.TemplatedFlowDataConfig, fieldMap map[string]any, templateID uint16, netflowV9 bool) (string, error) {
	names := selectPresentFlowFields(cfg, fieldMap)
	groups := fallbackAddressGroups(cfg, names)
	mask := fallbackAddressVariantMask(names, groups, fieldMap)
	templateID, err := fallbackVariantTemplateID(templateID, mask)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%t|%d", netflowV9, templateID)
	for _, name := range names {
		def := resolvedFieldDefinitionForAddressMask(name, cfg.Catalog[name], groups, mask)
		def = wireFieldDefinition(name, def, netflowV9)
		length := def.Length
		if length == 0 || length == 0xffff {
			encoded, err := encodeFallbackValue(name, def, fieldMap, templatedEncodingContext{netflowV9: netflowV9})
			if err == nil {
				length = ipfixFieldLength(def, encoded)
			}
		}
		fmt.Fprintf(&b, "|%s:%d:%d:%d:%d:%t", name, def.ID, length, def.Length, def.PEN, def.EnterpriseScoped)
	}
	return b.String(), nil
}

func buildFallbackPlan(cfg config.TemplatedFlowDataConfig, fieldMap map[string]any, templateID uint16, netflowV9 bool) (fallbackTemplatePlan, error) {
	names := selectPresentFlowFields(cfg, fieldMap)
	groups := fallbackAddressGroups(cfg, names)
	mask := fallbackAddressVariantMask(names, groups, fieldMap)
	templateID, err := fallbackVariantTemplateID(templateID, mask)
	if err != nil {
		return fallbackTemplatePlan{}, err
	}
	fields := make([]netflow.Field, 0, len(names))
	defs := make([]config.IPFIXFieldDefinition, 0, len(names))
	keptNames := make([]string, 0, len(names))
	for _, name := range names {
		def := resolvedFieldDefinitionForAddressMask(name, cfg.Catalog[name], groups, mask)
		def = wireFieldDefinition(name, def, netflowV9)
		encoded, err := encodeFallbackValue(name, def, fieldMap, templatedEncodingContext{netflowV9: netflowV9})
		if err != nil {
			return fallbackTemplatePlan{}, fmt.Errorf("encode field %q: %w", name, err)
		}
		fields = append(fields, netflow.Field{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        def.ID,
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

func buildFallbackValues(plan fallbackTemplatePlan, fieldMap map[string]any, encodingCtx templatedEncodingContext) (netflow.DataRecord, error) {
	values := make([]netflow.DataField, 0, len(plan.names))
	present := 0
	for i, name := range plan.names {
		def := plan.defs[i]
		if _, ok := fieldMap[name]; ok {
			present++
		}
		encoded, err := encodeFallbackValue(name, def, fieldMap, encodingCtx)
		if err != nil {
			return netflow.DataRecord{}, fmt.Errorf("encode field %q: %w", name, err)
		}
		values = append(values, netflow.DataField{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        plan.template.Fields[i].Type,
			Length:      plan.template.Fields[i].Length,
			Pen:         def.PEN,
			Value:       encoded,
		})
	}
	if len(values) == 0 || present == 0 {
		return netflow.DataRecord{}, fmt.Errorf("no encodable values found for templated packet")
	}
	return netflow.DataRecord{Values: values}, nil
}

func selectPresentFlowFields(cfg config.TemplatedFlowDataConfig, fieldMap map[string]any) []string {
	if len(cfg.Select) > 0 {
		names := make([]string, 0, len(cfg.Select))
		for _, name := range cfg.Select {
			if _, ok := cfg.Catalog[name]; !ok {
				continue
			}
			names = append(names, name)
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

func fallbackAddressGroups(cfg config.TemplatedFlowDataConfig, names []string) []string {
	fields := make([]event.SchemaField, 0, len(names))
	for _, name := range names {
		fields = append(fields, event.SchemaField{Name: name})
	}
	return schemaAddressGroups(cfg, fields)
}

func fallbackAddressVariantMask(names []string, groups []string, fieldMap map[string]any) uint64 {
	if len(groups) == 0 || len(fieldMap) == 0 {
		return 0
	}
	groupIndexes := make(map[string]int, len(groups))
	for i, group := range groups {
		groupIndexes[group] = i
	}
	var mask uint64
	for _, name := range names {
		group, ok := addressFieldGroup(name)
		if !ok {
			continue
		}
		index, ok := groupIndexes[group]
		if !ok {
			continue
		}
		if fieldValueIsIPv6(fieldMap[name]) {
			mask |= 1 << index
		}
	}
	return mask
}

func fallbackVariantTemplateID(baseTemplateID uint16, mask uint64) (uint16, error) {
	if uint64(baseTemplateID)+mask > 0xffff {
		return 0, fmt.Errorf("template id range %d..%d exceeds 65535", baseTemplateID, uint64(baseTemplateID)+mask)
	}
	return baseTemplateID + uint16(mask), nil
}

func encodeFallbackValue(name string, def config.IPFIXFieldDefinition, fieldMap map[string]any, encodingCtx templatedEncodingContext) ([]byte, error) {
	if val, ok := fieldMap[name]; ok {
		return encodeTemplatedValue(name, def, val, encodingCtx)
	}
	return defaultEncodedValue(def)
}

// buildTemplatedDataRecord picks fields from a runtime event and builds both the
// template and one matching data record.
func buildTemplatedDataRecord(cfg config.TemplatedFlowDataConfig, fieldMap map[string]any, templateID uint16, netflowV9 bool) (netflow.TemplateRecord, netflow.DataRecord, error) {
	names := selectFlowFields(cfg, fieldMap)
	return buildTemplatedDataRecordWithNames(cfg, fieldMap, names, templateID, templatedEncodingContext{netflowV9: netflowV9})
}

// buildTemplatedDataRecordWithNames uses an explicit field order, which matters
// when schema events already fixed the record layout.
func buildTemplatedDataRecordWithNames(cfg config.TemplatedFlowDataConfig, fieldMap map[string]any, names []string, templateID uint16, encodingCtx templatedEncodingContext) (netflow.TemplateRecord, netflow.DataRecord, error) {
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
		def = wireFieldDefinition(name, def, encodingCtx.netflowV9)
		encoded, err := encodeTemplatedValue(name, def, val, encodingCtx)
		if err != nil {
			return netflow.TemplateRecord{}, netflow.DataRecord{}, fmt.Errorf("encode field %q: %w", name, err)
		}
		templateFields = append(templateFields, netflow.Field{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        def.ID,
			Length:      ipfixFieldLength(def, encoded),
			Pen:         def.PEN,
		})
		values = append(values, netflow.DataField{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        def.ID,
			Length:      ipfixFieldLength(def, encoded),
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

func schemaFieldDefinition(cfg config.TemplatedFlowDataConfig, field event.SchemaField, netflowV9 bool, ipv6 bool) (config.IPFIXFieldDefinition, bool) {
	def, ok := cfg.Catalog[field.Name]
	if !ok {
		return config.IPFIXFieldDefinition{}, false
	}

	def = resolvedFieldDefinitionForFamily(field.Name, def, ipv6)
	if def.Name == "" {
		def.Name = field.Name
	}
	return def, true
}

func schemaFieldDefinitionForMask(cfg config.TemplatedFlowDataConfig, field event.SchemaField, groups []string, mask uint64) (config.IPFIXFieldDefinition, bool) {
	def, ok := cfg.Catalog[field.Name]
	if !ok {
		return config.IPFIXFieldDefinition{}, false
	}

	def = resolvedFieldDefinitionForAddressMask(field.Name, def, groups, mask)
	if def.Name == "" {
		def.Name = field.Name
	}
	return def, true
}

// buildTemplatedValues emits one data record using a preannounced template layout,
// filling missing fields with protocol-appropriate zero values.
func buildTemplatedValues(cfg config.TemplatedFlowDataConfig, fieldMap map[string]any, names []string, encodingCtx templatedEncodingContext, ipv6 bool) (netflow.DataRecord, error) {
	fields := make([]event.SchemaField, 0, len(names))
	for _, name := range names {
		fields = append(fields, event.SchemaField{Name: name, Role: "current"})
	}
	return buildTemplatedValuesFromSchemaFields(cfg, fieldMap, fields, encodingCtx, ipv6)
}

func buildTemplatedValuesFromSchemaFields(cfg config.TemplatedFlowDataConfig, fieldMap map[string]any, fields []event.SchemaField, encodingCtx templatedEncodingContext, ipv6 bool) (netflow.DataRecord, error) {
	return buildTemplatedValuesFromSchemaFieldsForMask(cfg, fieldMap, fields, encodingCtx, nil, boolAddressMask(ipv6))
}

func buildTemplatedValuesFromSchemaFieldsForMask(cfg config.TemplatedFlowDataConfig, fieldMap map[string]any, fields []event.SchemaField, encodingCtx templatedEncodingContext, groups []string, mask uint64) (netflow.DataRecord, error) {
	values := make([]netflow.DataField, 0, len(fields))
	for _, field := range fields {
		def, ok := schemaFieldDefinitionForMask(cfg, field, groups, mask)
		if !ok {
			continue
		}
		def = wireFieldDefinition(field.Name, def, encodingCtx.netflowV9)

		val, ok := fieldMap[field.Name]
		if !ok && field.Role == "static" && field.Value != nil {
			val = field.Value
			ok = true
		}
		var encoded []byte
		var err error
		if ok {
			encoded, err = encodeTemplatedValue(field.Name, def, val, encodingCtx)
			if err != nil {
				return netflow.DataRecord{}, fmt.Errorf("encode field %q: %w", field.Name, err)
			}
		} else {
			encoded, err = defaultEncodedValue(def)
			if err != nil {
				return netflow.DataRecord{}, fmt.Errorf("default field %q: %w", field.Name, err)
			}
		}

		values = append(values, netflow.DataField{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        def.ID,
			Length:      def.Length,
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
func buildTemplateRecordFromFields(cfg config.TemplatedFlowDataConfig, names []string, templateID uint16, netflowV9 bool, ipv6 bool) (netflow.TemplateRecord, error) {
	fields := make([]event.SchemaField, 0, len(names))
	for _, name := range names {
		fields = append(fields, event.SchemaField{Name: name, Role: "current"})
	}
	return buildTemplateRecordFromSchemaFields(cfg, fields, templateID, netflowV9, ipv6)
}

func buildTemplateRecordFromSchemaFields(cfg config.TemplatedFlowDataConfig, schemaFields []event.SchemaField, templateID uint16, netflowV9 bool, ipv6 bool) (netflow.TemplateRecord, error) {
	return buildTemplateRecordFromSchemaFieldsForMask(cfg, schemaFields, templateID, netflowV9, nil, boolAddressMask(ipv6))
}

func buildTemplateRecordFromSchemaFieldsForMask(cfg config.TemplatedFlowDataConfig, schemaFields []event.SchemaField, templateID uint16, netflowV9 bool, groups []string, mask uint64) (netflow.TemplateRecord, error) {
	fields := make([]netflow.Field, 0, len(schemaFields))
	for _, field := range schemaFields {
		def, ok := schemaFieldDefinitionForMask(cfg, field, groups, mask)
		if !ok {
			continue
		}
		def = wireFieldDefinition(field.Name, def, netflowV9)
		fields = append(fields, netflow.Field{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        def.ID,
			Length:      def.Length,
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
func selectFlowFields(cfg config.TemplatedFlowDataConfig, fieldMap map[string]any) []string {
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
	case "macAddress":
		switch v := val.(type) {
		case net.HardwareAddr:
			if len(v) != 6 {
				return nil, fmt.Errorf("expected 6-byte MAC address, got %d bytes", len(v))
			}
			return append([]byte(nil), v...), nil
		case []byte:
			if len(v) != 6 {
				return nil, fmt.Errorf("expected 6-byte MAC address, got %d bytes", len(v))
			}
			return append([]byte(nil), v...), nil
		case string:
			hw, err := net.ParseMAC(v)
			if err != nil {
				return nil, err
			}
			if len(hw) != 6 {
				return nil, fmt.Errorf("expected 6-byte MAC address, got %d bytes", len(hw))
			}
			return append([]byte(nil), hw...), nil
		default:
			return nil, fmt.Errorf("expected MAC string/[]byte, got %T", val)
		}
	case "unsigned8", "unsigned16", "unsigned32", "unsigned64":
		return encodeUnsigned(def.Type, val)
	case "signed8", "signed16", "signed32", "signed64":
		return encodeSigned(def.Type, val)
	case "boolean":
		if boolField(val) {
			return []byte{1}, nil
		}
		return []byte{0}, nil
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

func wireFieldDefinition(name string, def config.IPFIXFieldDefinition, netflowV9 bool) config.IPFIXFieldDefinition {
	if !netflowV9 {
		return def
	}
	switch name {
	case "start_time_unix":
		def.Name = "FIRST_SWITCHED"
		def.ID = netflow.NFV9_FIELD_FIRST_SWITCHED
		def.Length = 4
		def.Type = "unsigned32"
		def.Format = ""
	case "end_time_unix":
		def.Name = "LAST_SWITCHED"
		def.ID = netflow.NFV9_FIELD_LAST_SWITCHED
		def.Length = 4
		def.Type = "unsigned32"
		def.Format = ""
	}
	return def
}

func encodeTemplatedValue(name string, def config.IPFIXFieldDefinition, val any, encodingCtx templatedEncodingContext) ([]byte, error) {
	if encodingCtx.netflowV9 {
		switch name {
		case "start_time_unix":
			return encodeU32(encodingCtx.firstSwitched), nil
		case "end_time_unix":
			return encodeU32(encodingCtx.lastSwitched), nil
		}
	}
	return encodeIPFIXValue(def, val)
}

// defaultEncodedValue provides a zero representation for fields omitted from a
// templated event but still required by the selected template.
func defaultEncodedValue(def config.IPFIXFieldDefinition) ([]byte, error) {
	switch def.Type {
	case "ipv4Address":
		return make([]byte, 4), nil
	case "ipv6Address":
		return make([]byte, 16), nil
	case "macAddress":
		return make([]byte, 6), nil
	case "unsigned8", "signed8", "boolean":
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

// resolvedFieldDefinition upgrades address fields to their IPv6 definitions
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
	switch {
	case name == "agent_ip" && addr.Is6():
		def.Name = "exporterIPv6Address"
		def.ID = netflow.IPFIX_FIELD_exporterIPv6Address
		def.Length = 16
		def.Type = "ipv6Address"
	case name == "nat_src_addr" && isPostNATSourceIPv4Definition(def):
		if addr.Is6() {
			def.Name = "postNATSourceIPv6Address"
			def.ID = netflow.IPFIX_FIELD_postNATSourceIPv6Address
			def.Length = 16
			def.Type = "ipv6Address"
		}
	case name == "nat_dst_addr" && isPostNATDestinationIPv4Definition(def):
		if addr.Is6() {
			def.Name = "postNATDestinationIPv6Address"
			def.ID = netflow.IPFIX_FIELD_postNATDestinationIPv6Address
			def.Length = 16
			def.Type = "ipv6Address"
		}
	case isSourceAddressField(name) && isStandardSourceIPv4Definition(def):
		if addr.Is6() {
			def.Name = "sourceIPv6Address"
			def.ID = netflow.IPFIX_FIELD_sourceIPv6Address
			def.Length = 16
			def.Type = "ipv6Address"
		}
	case isDestinationAddressField(name) && isStandardDestinationIPv4Definition(def):
		if addr.Is6() {
			def.Name = "destinationIPv6Address"
			def.ID = netflow.IPFIX_FIELD_destinationIPv6Address
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
	switch {
	case name == "agent_ip":
		def.Name = "exporterIPv6Address"
		def.ID = netflow.IPFIX_FIELD_exporterIPv6Address
		def.Length = 16
		def.Type = "ipv6Address"
	case name == "nat_src_addr" && isPostNATSourceIPv4Definition(def):
		def.Name = "postNATSourceIPv6Address"
		def.ID = netflow.IPFIX_FIELD_postNATSourceIPv6Address
		def.Length = 16
		def.Type = "ipv6Address"
	case name == "nat_dst_addr" && isPostNATDestinationIPv4Definition(def):
		def.Name = "postNATDestinationIPv6Address"
		def.ID = netflow.IPFIX_FIELD_postNATDestinationIPv6Address
		def.Length = 16
		def.Type = "ipv6Address"
	case isSourceAddressField(name) && isStandardSourceIPv4Definition(def):
		def.Name = "sourceIPv6Address"
		def.ID = netflow.IPFIX_FIELD_sourceIPv6Address
		def.Length = 16
		def.Type = "ipv6Address"
	case isDestinationAddressField(name) && isStandardDestinationIPv4Definition(def):
		def.Name = "destinationIPv6Address"
		def.ID = netflow.IPFIX_FIELD_destinationIPv6Address
		def.Length = 16
		def.Type = "ipv6Address"
	}
	return def
}

func resolvedFieldDefinitionForAddressMask(name string, def config.IPFIXFieldDefinition, groups []string, mask uint64) config.IPFIXFieldDefinition {
	ipv6 := false
	if len(groups) == 0 {
		ipv6 = mask != 0
	} else if group, ok := addressFieldGroup(name); ok {
		for i, candidate := range groups {
			if candidate == group {
				ipv6 = mask&(1<<i) != 0
				break
			}
		}
	}
	return resolvedFieldDefinitionForFamily(name, def, ipv6)
}

func isSourceAddressField(name string) bool {
	return name == "src_addr" || strings.HasSuffix(name, "_src_addr")
}

func isDestinationAddressField(name string) bool {
	return name == "dst_addr" || strings.HasSuffix(name, "_dst_addr")
}

func isStandardSourceIPv4Definition(def config.IPFIXFieldDefinition) bool {
	return def.Type == "ipv4Address" && (def.ID == netflow.IPFIX_FIELD_sourceIPv4Address || def.Name == "sourceIPv4Address")
}

func isStandardDestinationIPv4Definition(def config.IPFIXFieldDefinition) bool {
	return def.Type == "ipv4Address" && (def.ID == netflow.IPFIX_FIELD_destinationIPv4Address || def.Name == "destinationIPv4Address")
}

func isPostNATSourceIPv4Definition(def config.IPFIXFieldDefinition) bool {
	return def.Type == "ipv4Address" && (def.ID == netflow.IPFIX_FIELD_postNATSourceIPv4Address || def.Name == "postNATSourceIPv4Address")
}

func isPostNATDestinationIPv4Definition(def config.IPFIXFieldDefinition) bool {
	return def.Type == "ipv4Address" && (def.ID == netflow.IPFIX_FIELD_postNATDestinationIPv4Address || def.Name == "postNATDestinationIPv4Address")
}

func addressFieldGroup(name string) (string, bool) {
	switch {
	case name == "src_addr" || name == "dst_addr":
		return "", true
	case strings.HasSuffix(name, "_src_addr"):
		return strings.TrimSuffix(name, "_src_addr"), true
	case strings.HasSuffix(name, "_dst_addr"):
		return strings.TrimSuffix(name, "_dst_addr"), true
	default:
		return "", false
	}
}

func schemaAddressGroups(cfg config.TemplatedFlowDataConfig, fields []event.SchemaField) []string {
	var groups []string
	seen := make(map[string]bool)
	for _, field := range fields {
		def, ok := cfg.Catalog[field.Name]
		if !ok {
			continue
		}
		if !isStandardSourceIPv4Definition(def) && !isStandardDestinationIPv4Definition(def) && !isPostNATSourceIPv4Definition(def) && !isPostNATDestinationIPv4Definition(def) {
			continue
		}
		group, ok := addressFieldGroup(field.Name)
		if !ok || seen[group] {
			continue
		}
		seen[group] = true
		groups = append(groups, group)
	}
	return groups
}

func fieldValueIsIPv6(val any) bool {
	ip, ok := val.(string)
	if !ok {
		return false
	}
	addr, err := netip.ParseAddr(ip)
	return err == nil && addr.Is6()
}

func boolAddressMask(ipv6 bool) uint64 {
	if ipv6 {
		return 1
	}
	return 0
}

func boolField(val any) bool {
	switch v := val.(type) {
	case bool:
		return v
	case uint64:
		return v != 0
	case uint32:
		return v != 0
	case uint16:
		return v != 0
	case uint8:
		return v != 0
	case int64:
		return v != 0
	case int:
		return v != 0
	case float64:
		return v != 0
	case json.Number:
		n, _ := v.Int64()
		return n != 0
	case string:
		return v == "true" || v == "1"
	default:
		return false
	}
}

// hasAddressField reports whether a schema needs dual IPv4/IPv6 template support.
func hasAddressField(names []string) bool {
	for _, name := range names {
		if isSourceAddressField(name) || isDestinationAddressField(name) {
			return true
		}
	}
	return false
}

// eventHasIPv6 checks the common address fields to determine which template family to use.
func eventHasIPv6(fields map[string]any) bool {
	for key, val := range fields {
		if !isSourceAddressField(key) && !isDestinationAddressField(key) {
			continue
		}
		ip, _ := val.(string)
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

// encodeU64 writes one uint64 in big-endian order.
func encodeU64(v uint64) []byte {
	return []byte{
		byte(v >> 56), byte(v >> 48), byte(v >> 40), byte(v >> 32),
		byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
	}
}

// ipfixFieldLength honors explicit field lengths and falls back to encoded size
// for variable-length definitions.
func ipfixFieldLength(def config.IPFIXFieldDefinition, encoded []byte) uint16 {
	if def.Length != 0 {
		return def.Length
	}
	if len(encoded) > 65535 {
		return 0xffff
	}
	return uint16(len(encoded))
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

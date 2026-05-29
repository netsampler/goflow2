package aggregate

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

// Aggregator receives processed events and optionally emits aggregated ones.
type Aggregator interface {
	InitEvents() ([]*event.Event, error)
	Process(evt *event.Event) ([]*event.Event, error)
	Flush() ([]*event.Event, error)
	Close() ([]*event.Event, error)
	Interval() time.Duration
}

// New builds the configured aggregation stage. Disabled aggregation keeps the input stream unchanged.
func New(cfg config.AggregatorConfig) (Aggregator, error) {
	if cfg.Passthrough {
		return schemaPassthrough{cfg: cfg}, nil
	}
	return NewStateful(cfg), nil
}

type schemaPassthrough struct {
	cfg config.AggregatorConfig
}

func (p schemaPassthrough) InitEvents() ([]*event.Event, error) {
	return schemaInitEvents(p.cfg)
}

func (p schemaPassthrough) Process(evt *event.Event) ([]*event.Event, error) {
	if len(p.cfg.StaticFields) == 0 {
		return []*event.Event{evt}, nil
	}
	if evt == nil {
		return []*event.Event{evt}, nil
	}
	out := *evt
	out.Fields = cloneFields(evt.Fields)
	for key, val := range p.cfg.StaticFields {
		out.Fields[key] = val
	}
	return []*event.Event{&out}, nil
}

func (schemaPassthrough) Flush() ([]*event.Event, error) { return nil, nil }
func (schemaPassthrough) Close() ([]*event.Event, error) { return nil, nil }
func (schemaPassthrough) Interval() time.Duration        { return 0 }

type aggregateRecord struct {
	Fields    map[string]any
	FirstSeen time.Time
	LastSeen  time.Time
	Dirty     bool
}

type missingAggregationKeyError struct {
	Key string
}

func (e *missingAggregationKeyError) Error() string {
	return fmt.Sprintf("missing aggregation key field %q", e.Key)
}

// Add merges a delta record into the accumulator while preserving first/last
// visibility windows and dirty-state for periodic export.
func (r *aggregateRecord) Add(delta aggregateRecord, existed bool) error {
	if !existed {
		delta.Dirty = true
		*r = delta
		return nil
	}
	if r.Fields == nil {
		r.Fields = make(map[string]any)
	}
	mergeFields(r.Fields, delta.Fields)
	if delta.FirstSeen.Before(r.FirstSeen) {
		r.FirstSeen = delta.FirstSeen
	}
	if delta.LastSeen.After(r.LastSeen) {
		r.LastSeen = delta.LastSeen
	}
	r.Dirty = true
	return nil
}

// Stateful keeps all aggregation policy in one place.
//
// The old aggregator relied on FlowStore TTL only, which was fine for
// "flush when idle" but not enough once ReFlow needed:
// - idle-based flush
// - max lifetime flush
// - periodic snapshot export
// - periodic export with optional bucket reset
// - silent stale bucket cleanup without exporting
//
// Keeping those timers together here makes the behavior explicit and easier to
// reason about than mixing TTL expiration with separate periodic logic.
type Stateful struct {
	cfg            config.AggregatorConfig
	recordCapacity int

	mu              sync.Mutex
	state           map[string]aggregateRecord
	startedAt       time.Time
	lastPeriodicRun time.Time
}

func NewStateful(cfg config.AggregatorConfig) *Stateful {
	return &Stateful{
		cfg:            cfg,
		recordCapacity: aggregateRecordCapacity(cfg),
		state:          make(map[string]aggregateRecord),
		startedAt:      time.Now(),
	}
}

// InitEvents emits one schema control event so downstream templated encoders can
// advertise aggregated streams before the first bucket flushes.
func (a *Stateful) InitEvents() ([]*event.Event, error) {
	return schemaInitEvents(a.cfg)
}

func schemaInitEvents(cfg config.AggregatorConfig) ([]*event.Event, error) {
	fieldNames := orderedSchemaFields(cfg)
	if len(fieldNames) == 0 {
		return nil, nil
	}
	return []*event.Event{
		{
			ReceivedAt: time.Now().UTC(),
			Kind:       "control",
			Stream:     cfg.Stream,
			Source: event.SourceMetadata{
				Type: "aggregator",
			},
			Control: &event.ControlMetadata{
				Type:   "schema",
				Stream: cfg.Stream,
			},
			Payload: event.AggregationSchema{
				Stream:         cfg.Stream,
				FieldNames:     fieldNames,
				Fields:         schemaFields(cfg),
				KeyFields:      append([]string(nil), cfg.KeyFields...),
				SumFields:      append([]string(nil), cfg.Sum...),
				FirstFields:    append([]string(nil), cfg.First...),
				CurrentFields:  append([]string(nil), cfg.Current...),
				MinFields:      append([]string(nil), cfg.Min...),
				MaxFields:      append([]string(nil), cfg.Max...),
				AndFields:      append([]string(nil), cfg.And...),
				Match:          cloneStringMap(cfg.Match),
				StaticFields:   cloneFields(cfg.StaticFields),
				BaseTemplateID: cfg.TemplateID,
			},
		},
	}, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, val := range in {
		out[key] = val
	}
	return out
}

// Process adds one event into the keyed aggregation state. Missing key fields
// are treated as non-matches rather than hard pipeline failures.
func (a *Stateful) Process(evt *event.Event) ([]*event.Event, error) {
	if evt != nil && evt.Kind == "control" {
		return []*event.Event{evt}, nil
	}
	key, record, err := aggregateFromEvent(a.cfg, a.recordCapacity, evt)
	if err != nil {
		var missingKeyErr *missingAggregationKeyError
		if errors.As(err, &missingKeyErr) {
			slog.Warn(
				"dropping aggregate event with missing key field",
				slog.String("stream", a.cfg.Stream),
				slog.String("key", missingKeyErr.Key),
			)
			return nil, nil
		}
		return nil, err
	}
	a.mu.Lock()
	current, exists := a.state[key]
	if err := current.Add(record, exists); err != nil {
		a.mu.Unlock()
		return nil, err
	}
	a.state[key] = current
	a.mu.Unlock()
	return nil, nil
}

// orderedSchemaFields produces a stable field order for schema announcements and
// templated encoders that depend on deterministic field positions.
func orderedSchemaFields(cfg config.AggregatorConfig) []string {
	if cfg.FieldsConfigured {
		seen := make(map[string]struct{})
		out := make([]string, 0, len(cfg.Fields))
		for _, field := range cfg.Fields {
			if field.Name == "" {
				continue
			}
			if _, ok := seen[field.Name]; ok {
				continue
			}
			seen[field.Name] = struct{}{}
			out = append(out, field.Name)
		}
		return out
	}

	seen := make(map[string]struct{})
	var out []string
	appendField := func(field string) {
		if field == "" {
			return
		}
		if _, ok := seen[field]; ok {
			return
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	for _, field := range cfg.KeyFields {
		appendField(field)
	}
	for _, field := range cfg.Sum {
		appendField(field)
	}
	for _, field := range cfg.First {
		appendField(field)
	}
	for _, field := range cfg.Current {
		appendField(field)
	}
	for _, field := range cfg.Min {
		appendField(field)
	}
	for _, field := range cfg.Max {
		appendField(field)
	}
	for _, field := range cfg.And {
		appendField(field)
	}
	appendField("start_time_unix")
	appendField("end_time_unix")
	staticFields := make([]string, 0, len(cfg.StaticFields))
	for field := range cfg.StaticFields {
		staticFields = append(staticFields, field)
	}
	sort.Strings(staticFields)
	for _, field := range staticFields {
		appendField(field)
	}
	return out
}

func schemaFields(cfg config.AggregatorConfig) []event.SchemaField {
	if cfg.FieldsConfigured {
		out := make([]event.SchemaField, 0, len(cfg.Fields))
		seen := make(map[string]struct{}, len(cfg.Fields))
		for _, field := range cfg.Fields {
			if field.Name == "" {
				continue
			}
			if _, ok := seen[field.Name]; ok {
				continue
			}
			seen[field.Name] = struct{}{}
			out = append(out, event.SchemaField{
				Role:  field.Role,
				Name:  field.Name,
				Value: field.Value,
			})
		}
		return out
	}

	names := orderedSchemaFields(cfg)
	out := make([]event.SchemaField, 0, len(names))
	roleByName := make(map[string]string, len(names))
	for _, field := range cfg.KeyFields {
		roleByName[field] = "key"
	}
	for _, field := range cfg.Sum {
		roleByName[field] = "sum"
	}
	for _, field := range cfg.First {
		roleByName[field] = "first"
	}
	for _, field := range cfg.Current {
		roleByName[field] = "current"
	}
	for _, field := range cfg.Min {
		roleByName[field] = "min"
	}
	for _, field := range cfg.Max {
		roleByName[field] = "max"
	}
	for _, field := range cfg.And {
		roleByName[field] = "and"
	}
	for _, name := range names {
		role := roleByName[name]
		if _, ok := cfg.StaticFields[name]; ok {
			role = "static"
		}
		if role == "" {
			role = "current"
		}
		out = append(out, event.SchemaField{
			Role:  role,
			Name:  name,
			Value: cfg.StaticFields[name],
		})
	}
	return out
}

// Flush evaluates timer-based export policies against the current state map.
func (a *Stateful) Flush() ([]*event.Event, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.flushAt(time.Now(), false), nil
}

// Close exports all remaining buckets regardless of timer state.
func (a *Stateful) Close() ([]*event.Event, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// On shutdown ReFlow should not silently drop aggregate state. Close exports
	// every remaining bucket one last time regardless of dirty state.
	out := make([]*event.Event, 0, len(a.state))
	for key, record := range a.state {
		out = append(out, buildAggregatedEvent(a.cfg.Stream, key, record))
	}
	return out, nil
}

// Interval reports the smallest configured timer so the runtime knows how often
// to wake this aggregator worker for flush evaluation.
func (a *Stateful) Interval() time.Duration {
	var min time.Duration
	add := func(ms int) {
		if ms <= 0 {
			return
		}
		d := time.Duration(ms) * time.Millisecond
		if min == 0 || d < min {
			min = d
		}
	}
	add(a.cfg.Window.IdleFlushAfter)
	add(a.cfg.Window.MaxFlushAfter)
	add(a.cfg.Window.IdleEraseAfter)
	add(a.cfg.Periodic.Every)
	return min
}

// flushAt evaluates every bucket against the configured timers.
//
// Evaluation order:
// 1. idle window flush
// 2. max lifetime window flush
// 3. periodic snapshot export
// 4. idle erase without export
//
// That order is intentional:
//   - a bucket that qualifies for a real window flush should be exported, not only
//     snapshotted and kept around
//   - silent erase is last so it only applies when no export trigger fired
func (a *Stateful) flushAt(now time.Time, closing bool) []*event.Event {
	out := make([]*event.Event, 0, len(a.state))
	periodicDue := a.periodicDue(now)
	for key, record := range a.state {
		if closing {
			out = append(out, buildAggregatedEvent(a.cfg.Stream, key, record))
			continue
		}

		if a.shouldFlushIdle(record, now) || a.shouldFlushMax(record, now) {
			out = append(out, buildAggregatedEvent(a.cfg.Stream, key, record))
			delete(a.state, key)
			continue
		}

		if periodicDue && a.shouldEmitPeriodic(record) {
			out = append(out, buildAggregatedEvent(a.cfg.Stream, key, record))
			if a.cfg.Periodic.ResetBuckets {
				delete(a.state, key)
			} else {
				record.Dirty = false
				a.state[key] = record
			}
			continue
		}

		if a.shouldEraseIdle(record, now) {
			delete(a.state, key)
		}
	}
	if periodicDue {
		a.lastPeriodicRun = now
	}
	return out
}

// shouldFlushIdle exports a bucket after it has been inactive long enough.
func (a *Stateful) shouldFlushIdle(record aggregateRecord, now time.Time) bool {
	if a.cfg.Window.IdleFlushAfter <= 0 {
		return false
	}
	return now.Sub(record.LastSeen) >= time.Duration(a.cfg.Window.IdleFlushAfter)*time.Millisecond
}

// shouldFlushMax enforces an absolute bucket lifetime even when updates keep arriving.
func (a *Stateful) shouldFlushMax(record aggregateRecord, now time.Time) bool {
	if a.cfg.Window.MaxFlushAfter <= 0 {
		return false
	}
	return now.Sub(record.FirstSeen) >= time.Duration(a.cfg.Window.MaxFlushAfter)*time.Millisecond
}

// Periodic export is intentionally driven by the aggregate worker ticker. The
// dirty bit prevents the same untouched bucket from being emitted over and over
// when periodic snapshots are enabled.
func (a *Stateful) shouldEmitPeriodic(record aggregateRecord) bool {
	return record.Dirty
}

// shouldEraseIdle drops stale buckets that should disappear silently instead of exporting.
func (a *Stateful) shouldEraseIdle(record aggregateRecord, now time.Time) bool {
	if a.cfg.Window.IdleEraseAfter <= 0 {
		return false
	}
	return now.Sub(record.LastSeen) >= time.Duration(a.cfg.Window.IdleEraseAfter)*time.Millisecond
}

// periodicDue ensures snapshot emission follows the aggregator's own wall clock
// rather than event timestamps.
func (a *Stateful) periodicDue(now time.Time) bool {
	if a.cfg.Periodic.Every <= 0 {
		return false
	}
	if a.lastPeriodicRun.IsZero() {
		return now.Sub(a.startedAt) >= time.Duration(a.cfg.Periodic.Every)*time.Millisecond
	}
	return now.Sub(a.lastPeriodicRun) >= time.Duration(a.cfg.Periodic.Every)*time.Millisecond
}

// aggregateFromEvent converts one processed event into the stored aggregation
// representation used inside a bucket.
func aggregateFromEvent(cfg config.AggregatorConfig, recordCapacity int, evt *event.Event) (string, aggregateRecord, error) {
	fields := evt.Fields
	if fields == nil {
		return "", aggregateRecord{}, fmt.Errorf("event fields are empty")
	}

	key, err := buildKey(evt, cfg.KeyFields)
	if err != nil {
		return "", aggregateRecord{}, err
	}

	// Aggregation window timers are runtime policy, not event timestamps.
	//
	// Using evt.ReceivedAt here would make replayed traffic or synthetic test
	// events look instantly ancient, which would trigger max/idle flushes
	// incorrectly. The actual flow timestamps remain in start_time_unix and
	// end_time_unix; the bucket lifecycle timers should follow wall-clock time.
	now := time.Now()

	recordFields := make(map[string]any, recordCapacity)
	for key, val := range cfg.StaticFields {
		recordFields[key] = val
	}
	for _, keyField := range cfg.KeyFields {
		if val, ok := eventFieldValue(evt, keyField); ok {
			recordFields[keyField] = val
		}
	}
	for _, sumField := range cfg.Sum {
		recordFields[sumField] = sumValue{Value: int64EventField(evt, sumField)}
	}
	for _, firstField := range cfg.First {
		if val, ok := eventFieldValue(evt, firstField); ok {
			recordFields[firstField] = firstValue{Value: val}
		}
	}
	for _, currentField := range cfg.Current {
		if val, ok := eventFieldValue(evt, currentField); ok {
			recordFields[currentField] = currentValue{Value: val}
		}
	}
	for _, minField := range cfg.Min {
		if val, ok := eventFieldValue(evt, minField); ok {
			recordFields[minField] = minValue{Value: val}
		}
	}
	for _, maxField := range cfg.Max {
		if val, ok := eventFieldValue(evt, maxField); ok {
			recordFields[maxField] = maxValue{Value: val}
		}
	}
	for _, andField := range cfg.And {
		if val, ok := eventFieldValue(evt, andField); ok {
			recordFields[andField] = andValue{Value: val}
		}
	}
	seedTimestamps(recordFields, fields, now)

	return key, aggregateRecord{
		Fields:    recordFields,
		FirstSeen: now,
		LastSeen:  now,
	}, nil
}

func aggregateRecordCapacity(cfg config.AggregatorConfig) int {
	return len(cfg.StaticFields) + len(cfg.KeyFields) + len(cfg.Sum) + len(cfg.First) + len(cfg.Current) + len(cfg.Min) + len(cfg.Max) + len(cfg.And) + 2
}

// seedTimestamps ensures aggregates always have start/end fields even when the
// input record omitted explicit flow timing.
func seedTimestamps(dst, src map[string]any, now time.Time) {
	start := timestampFieldOrNow(src, "start_time_unix", now)
	end := timestampFieldOrNow(src, "end_time_unix", now)
	dst["start_time_unix"] = start
	dst["end_time_unix"] = end
}

// buildKey joins the configured key fields into one stable bucket identifier.
func buildKey(evt *event.Event, keyFields []string) (string, error) {
	if len(keyFields) == 0 {
		return "__global__", nil
	}
	var b strings.Builder
	b.Grow(len(keyFields) * 16)
	for i, key := range keyFields {
		val, ok := eventFieldValue(evt, key)
		if !ok {
			return "", &missingAggregationKeyError{Key: key}
		}
		if i > 0 {
			b.WriteByte('|')
		}
		writeKeyValue(&b, val)
	}
	return b.String(), nil
}

func writeKeyValue(b *strings.Builder, val any) {
	switch v := val.(type) {
	case string:
		b.WriteString(v)
	case uint64:
		b.WriteString(strconv.FormatUint(v, 10))
	case uint32:
		b.WriteString(strconv.FormatUint(uint64(v), 10))
	case uint16:
		b.WriteString(strconv.FormatUint(uint64(v), 10))
	case uint8:
		b.WriteString(strconv.FormatUint(uint64(v), 10))
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
	case int:
		b.WriteString(strconv.Itoa(v))
	case float64:
		b.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
	case bool:
		b.WriteString(strconv.FormatBool(v))
	default:
		fmt.Fprint(b, v)
	}
}

// buildAggregatedEvent materializes one bucket into a regular runtime event.
func buildAggregatedEvent(stream, key string, record aggregateRecord) *event.Event {
	fields := cloneFields(record.Fields)

	return &event.Event{
		ReceivedAt: time.Now(),
		Stream:     stream,
		Source: event.SourceMetadata{
			Type: "aggregated_flow",
		},
		Aggregation: &event.AggregationMetadata{
			Key:           key,
			FirstSeenUnix: record.FirstSeen.UnixMilli(),
			LastSeenUnix:  record.LastSeen.UnixMilli(),
		},
		Fields: fields,
	}
}

// cloneFields unwraps internal accumulator marker types back into plain values
// suitable for encoding and logging.
func cloneFields(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+3)
	for key, val := range in {
		switch typed := val.(type) {
		case sumValue:
			out[key] = typed.Value
		case firstValue:
			out[key] = typed.Value
		case currentValue:
			out[key] = typed.Value
		case minValue:
			out[key] = typed.Value
		case maxValue:
			out[key] = typed.Value
		case andValue:
			out[key] = typed.Value
		default:
			out[key] = val
		}
	}
	return out
}

type firstValue struct{ Value any }
type sumValue struct{ Value any }
type currentValue struct{ Value any }
type minValue struct{ Value any }
type maxValue struct{ Value any }
type andValue struct{ Value any }

// mergeFields applies the configured aggregation semantics by field wrapper type:
// sum values add, first values stick, current values overwrite, min/max compare,
// and values bitwise-AND.
func mergeFields(dst, src map[string]any) {
	for key, val := range src {
		if key == "start_time_unix" {
			dst[key] = minTimestamp(int64FromAny(dst[key]), int64FromAny(val))
			continue
		}
		if key == "end_time_unix" {
			dst[key] = maxTimestamp(int64FromAny(dst[key]), int64FromAny(val))
			continue
		}
		switch incoming := val.(type) {
		case sumValue:
			dst[key] = sumValue{Value: sumValueOf(dst[key]) + int64FromAny(incoming.Value)}
			continue
		case firstValue:
			if _, exists := dst[key]; !exists {
				dst[key] = incoming
			}
		case currentValue:
			dst[key] = incoming
		case minValue:
			dst[key] = minValue{Value: minAny(dst[key], incoming.Value)}
		case maxValue:
			dst[key] = maxValue{Value: maxAny(dst[key], incoming.Value)}
		case andValue:
			dst[key] = andValue{Value: andAny(dst[key], incoming.Value)}
		default:
			dst[key] = val
		}
	}
}

// sumValueOf unwraps sum markers and normalizes plain integers into an additive value.
func sumValueOf(val any) int64 {
	switch typed := val.(type) {
	case sumValue:
		return int64FromAny(typed.Value)
	default:
		return int64FromAny(val)
	}
}

func minAny(current, incoming any) any {
	if wrapped, exists := current.(minValue); exists {
		if int64FromAny(incoming) < int64FromAny(wrapped.Value) {
			return incoming
		}
		return wrapped.Value
	}
	if int64FromAny(incoming) < int64FromAny(current) {
		return incoming
	}
	return current
}

func maxAny(current, incoming any) any {
	if wrapped, exists := current.(maxValue); exists {
		if int64FromAny(incoming) > int64FromAny(wrapped.Value) {
			return incoming
		}
		return wrapped.Value
	}
	if int64FromAny(incoming) > int64FromAny(current) {
		return incoming
	}
	return current
}

func andAny(current, incoming any) any {
	if wrapped, exists := current.(andValue); exists {
		return uint32FromAny(wrapped.Value) & uint32FromAny(incoming)
	}
	return uint32FromAny(current) & uint32FromAny(incoming)
}

// timestampFieldOrNow prefers an existing field timestamp and falls back to the
// current wall clock when the input record did not provide one.
func timestampFieldOrNow(fields map[string]any, key string, now time.Time) int64 {
	if fields != nil {
		if val, ok := fields[key]; ok {
			ts := int64FromAny(val)
			if ts != 0 {
				return ts
			}
		}
	}
	return now.UnixMilli()
}

// minTimestamp keeps the earlier non-zero timestamp.
func minTimestamp(a, b int64) int64 {
	if a == 0 {
		return b
	}
	if b == 0 || a <= b {
		return a
	}
	return b
}

// maxTimestamp keeps the later non-zero timestamp.
func maxTimestamp(a, b int64) int64 {
	if a == 0 {
		return b
	}
	if b == 0 || a >= b {
		return a
	}
	return b
}

// int64EventField reads one numeric field from the event field map or metadata.
func int64EventField(evt *event.Event, key string) int64 {
	val, ok := eventFieldValue(evt, key)
	if !ok {
		return 0
	}
	return int64FromAny(val)
}

func eventFieldValue(evt *event.Event, key string) (any, bool) {
	if evt == nil {
		return nil, false
	}
	switch key {
	case "agent_ip":
		if agentIP, ok := metadataAgentIP(evt); ok {
			if agentIPv4, _ := agentIPByFamily(agentIP); agentIPv4 != "" {
				return agentIPv4, true
			}
			return nil, false
		}
	case "agent_ipv6":
		if agentIP, ok := metadataAgentIP(evt); ok {
			if _, agentIPv6 := agentIPByFamily(agentIP); agentIPv6 != "" {
				return agentIPv6, true
			}
			return nil, false
		}
	case "source_id":
		if evt.SFlow != nil && evt.SFlow.SourceID != 0 {
			return evt.SFlow.SourceID, true
		}
		if evt.Source.SourceIDSet || evt.Source.SourceID != 0 {
			return evt.Source.SourceID, true
		}
	case "sampling_rate":
		if evt.SFlow != nil && evt.SFlow.SamplingRate != 0 {
			return evt.SFlow.SamplingRate, true
		}
		if evt.Source.Sampling != nil {
			return evt.Source.Sampling.Rate, true
		}
	case "sample_pool":
		if evt.SFlow != nil && evt.SFlow.SamplePool != 0 {
			return evt.SFlow.SamplePool, true
		}
		if evt.Source.Sampling != nil {
			return evt.Source.Sampling.SamplePool, true
		}
	case "drops":
		if evt.SFlow != nil && evt.SFlow.Drops != 0 {
			return evt.SFlow.Drops, true
		}
		if evt.Source.Sampling != nil {
			return evt.Source.Sampling.Drops, true
		}
	}
	return fieldValue(evt.Fields, key)
}

func metadataAgentIP(evt *event.Event) (string, bool) {
	if evt.SFlow != nil && evt.SFlow.AgentIP != "" {
		return evt.SFlow.AgentIP, true
	}
	if evt.Source.AgentIP != "" {
		return evt.Source.AgentIP, true
	}
	return "", false
}

func agentIPByFamily(raw string) (string, string) {
	if raw == "" {
		return "", ""
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return raw, ""
	}
	if addr.Is4() {
		return raw, ""
	}
	return "", raw
}

// fieldValue reads flat field names first, then dotted paths through nested
// maps and slices. This lets aggregate configs key on structured values without
// forcing the event field map itself to be flat.
func fieldValue(fields map[string]any, key string) (any, bool) {
	if fields == nil {
		return nil, false
	}
	if val, ok := fields[key]; ok {
		return val, true
	}
	if !strings.Contains(key, ".") {
		return nil, false
	}
	parts := strings.Split(key, ".")
	var current any = fields
	for _, part := range parts {
		val, ok := nestedFieldValue(current, part)
		if !ok {
			return nil, false
		}
		current = val
	}
	return current, true
}

func nestedFieldValue(current any, part string) (any, bool) {
	switch typed := current.(type) {
	case map[string]any:
		val, ok := typed[part]
		return val, ok
	case []map[string]any:
		index, ok := pathIndex(part, len(typed))
		if !ok {
			return nil, false
		}
		return typed[index], true
	case []any:
		index, ok := pathIndex(part, len(typed))
		if !ok {
			return nil, false
		}
		return typed[index], true
	default:
		return nil, false
	}
}

func pathIndex(part string, length int) (int, bool) {
	index, err := strconv.Atoi(part)
	if err != nil || index < 0 || index >= length {
		return 0, false
	}
	return index, true
}

// uint32FromAny normalizes the small set of number shapes aggregation expects.
func uint32FromAny(val any) uint32 {
	switch v := val.(type) {
	case uint32:
		return v
	case uint64:
		return uint32(v)
	case int:
		return uint32(v)
	case int64:
		return uint32(v)
	case float64:
		return uint32(v)
	default:
		return 0
	}
}

// int64FromAny normalizes the small set of number shapes aggregation expects.
func int64FromAny(val any) int64 {
	switch v := val.(type) {
	case int64:
		return v
	case uint64:
		return int64(v)
	case uint32:
		return int64(v)
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}

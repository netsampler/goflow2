package aggregate

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
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
	if !cfg.Enabled {
		return passthrough{}, nil
	}
	return NewStateful(cfg), nil
}

type passthrough struct{}

func (passthrough) InitEvents() ([]*event.Event, error)              { return nil, nil }
func (passthrough) Process(evt *event.Event) ([]*event.Event, error) { return []*event.Event{evt}, nil }
func (passthrough) Flush() ([]*event.Event, error)                   { return nil, nil }
func (passthrough) Close() ([]*event.Event, error)                   { return nil, nil }
func (passthrough) Interval() time.Duration                          { return 0 }

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
	if !a.cfg.Enabled {
		return nil, nil
	}
	fieldNames := orderedSchemaFields(a.cfg)
	return []*event.Event{
		{
			ReceivedAt: time.Now().UTC(),
			Kind:       "control",
			Stream:     a.cfg.Stream,
			Source: event.SourceMetadata{
				Type: "aggregator",
			},
			Control: &event.ControlMetadata{
				Type:   "schema",
				Stream: a.cfg.Stream,
			},
			Payload: event.AggregationSchema{
				Stream:         a.cfg.Stream,
				FieldNames:     fieldNames,
				KeyFields:      append([]string(nil), a.cfg.KeyFields...),
				SumFields:      append([]string(nil), a.cfg.Sum...),
				FirstFields:    append([]string(nil), a.cfg.First...),
				CurrentFields:  append([]string(nil), a.cfg.Current...),
				StaticFields:   cloneFields(a.cfg.StaticFields),
				BaseTemplateID: a.cfg.TemplateID,
			},
		},
	}, nil
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
	if !a.cfg.Enabled {
		return 0
	}
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

	key, err := buildKey(fields, cfg.KeyFields)
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
		if val, ok := fieldValue(fields, keyField); ok {
			recordFields[keyField] = val
		}
	}
	for _, sumField := range cfg.Sum {
		recordFields[sumField] = sumValue{Value: int64Field(fields, sumField)}
	}
	for _, firstField := range cfg.First {
		if val, ok := fieldValue(fields, firstField); ok {
			recordFields[firstField] = firstValue{Value: val}
		}
	}
	for _, currentField := range cfg.Current {
		if val, ok := fieldValue(fields, currentField); ok {
			recordFields[currentField] = currentValue{Value: val}
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
	return len(cfg.StaticFields) + len(cfg.KeyFields) + len(cfg.Sum) + len(cfg.First) + len(cfg.Current) + 2
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
func buildKey(fields map[string]any, keyFields []string) (string, error) {
	if len(keyFields) == 0 {
		return "__global__", nil
	}
	var b strings.Builder
	b.Grow(len(keyFields) * 16)
	for i, key := range keyFields {
		val, ok := fieldValue(fields, key)
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
		b.WriteString(fmt.Sprint(v))
	}
}

// buildAggregatedEvent materializes one bucket into a regular runtime event.
func buildAggregatedEvent(stream, key string, record aggregateRecord) *event.Event {
	fields := cloneFields(record.Fields)
	fields["aggregation_key"] = key
	fields["first_seen_unix"] = record.FirstSeen.UnixMilli()
	fields["last_seen_unix"] = record.LastSeen.UnixMilli()

	return &event.Event{
		ReceivedAt: time.Now(),
		Stream:     stream,
		Source: event.SourceMetadata{
			Type: "aggregated_flow",
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
		default:
			out[key] = val
		}
	}
	return out
}

type firstValue struct{ Value any }
type sumValue struct{ Value any }
type currentValue struct{ Value any }

// mergeFields applies the configured aggregation semantics by field wrapper type:
// sum values add, first values stick, current values overwrite.
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

// int64Field reads one numeric field from the generic field map.
func int64Field(fields map[string]any, key string) int64 {
	val, ok := fieldValue(fields, key)
	if !ok {
		return 0
	}
	return int64FromAny(val)
}

// fieldValue reads flat field names first, then dotted paths through nested
// maps and slices. This lets aggregate configs key on packet layers such as
// "ip_layers.0.src_addr" without forcing the event field map itself to be flat.
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

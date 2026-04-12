package aggregate

import (
	"errors"
	"fmt"
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
	cfg config.AggregatorConfig

	mu              sync.Mutex
	state           map[string]aggregateRecord
	startedAt       time.Time
	lastPeriodicRun time.Time
}

func NewStateful(cfg config.AggregatorConfig) *Stateful {
	return &Stateful{
		cfg:       cfg,
		state:     make(map[string]aggregateRecord),
		startedAt: time.Now(),
	}
}

func (a *Stateful) InitEvents() ([]*event.Event, error) {
	if !a.cfg.Enabled {
		return nil, nil
	}
	fieldNames := orderedSchemaFields(a.cfg)
	return []*event.Event{
		{
			ReceivedAt: time.Now().UTC(),
			Kind:       "control",
			Source: event.SourceMetadata{
				Type: "aggregator",
			},
			Control: &event.ControlMetadata{
				Type:   "schema",
				Stream: "flow_data",
			},
			Payload: event.AggregationSchema{
				Stream:        "flow_data",
				FieldNames:    fieldNames,
				KeyFields:     append([]string(nil), a.cfg.KeyFields...),
				SumFields:     append([]string(nil), a.cfg.Sum...),
				FirstFields:   append([]string(nil), a.cfg.First...),
				CurrentFields: append([]string(nil), a.cfg.Current...),
				StaticFields:  cloneFields(a.cfg.StaticFields),
			},
		},
	}, nil
}

func (a *Stateful) Process(evt *event.Event) ([]*event.Event, error) {
	if evt != nil && evt.Kind == "control" {
		return []*event.Event{evt}, nil
	}
	key, record, err := aggregateFromEvent(a.cfg, evt)
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
	for field := range cfg.StaticFields {
		appendField(field)
	}
	return out
}

func (a *Stateful) Flush() ([]*event.Event, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.flushAt(time.Now(), false), nil
}

func (a *Stateful) Close() ([]*event.Event, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// On shutdown ReFlow should not silently drop aggregate state. Close exports
	// every remaining bucket one last time regardless of dirty state.
	out := make([]*event.Event, 0, len(a.state))
	for key, record := range a.state {
		out = append(out, buildAggregatedEvent(key, record))
	}
	return out, nil
}

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
			out = append(out, buildAggregatedEvent(key, record))
			continue
		}

		if a.shouldFlushIdle(record, now) || a.shouldFlushMax(record, now) {
			out = append(out, buildAggregatedEvent(key, record))
			delete(a.state, key)
			continue
		}

		if periodicDue && a.shouldEmitPeriodic(record) {
			out = append(out, buildAggregatedEvent(key, record))
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

func (a *Stateful) shouldFlushIdle(record aggregateRecord, now time.Time) bool {
	if a.cfg.Window.IdleFlushAfter <= 0 {
		return false
	}
	return now.Sub(record.LastSeen) >= time.Duration(a.cfg.Window.IdleFlushAfter)*time.Millisecond
}

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

func (a *Stateful) shouldEraseIdle(record aggregateRecord, now time.Time) bool {
	if a.cfg.Window.IdleEraseAfter <= 0 {
		return false
	}
	return now.Sub(record.LastSeen) >= time.Duration(a.cfg.Window.IdleEraseAfter)*time.Millisecond
}

func (a *Stateful) periodicDue(now time.Time) bool {
	if a.cfg.Periodic.Every <= 0 {
		return false
	}
	if a.lastPeriodicRun.IsZero() {
		return now.Sub(a.startedAt) >= time.Duration(a.cfg.Periodic.Every)*time.Millisecond
	}
	return now.Sub(a.lastPeriodicRun) >= time.Duration(a.cfg.Periodic.Every)*time.Millisecond
}

func aggregateFromEvent(cfg config.AggregatorConfig, evt *event.Event) (string, aggregateRecord, error) {
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

	recordFields := make(map[string]any)
	for key, val := range cfg.StaticFields {
		recordFields[key] = val
	}
	for _, keyField := range cfg.KeyFields {
		if val, ok := fields[keyField]; ok {
			recordFields[keyField] = val
		}
	}
	for _, sumField := range cfg.Sum {
		recordFields[sumField] = sumValue{Value: int64Field(fields, sumField)}
	}
	for _, firstField := range cfg.First {
		if val, ok := fields[firstField]; ok {
			recordFields[firstField] = firstValue{Value: val}
		}
	}
	for _, currentField := range cfg.Current {
		if val, ok := fields[currentField]; ok {
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

func seedTimestamps(dst, src map[string]any, now time.Time) {
	start := timestampFieldOrNow(src, "start_time_unix", now)
	end := timestampFieldOrNow(src, "end_time_unix", now)
	dst["start_time_unix"] = start
	dst["end_time_unix"] = end
}

func buildKey(fields map[string]any, keyFields []string) (string, error) {
	if len(keyFields) == 0 {
		return "__global__", nil
	}
	parts := make([]string, 0, len(keyFields))
	for _, key := range keyFields {
		val, ok := fields[key]
		if !ok {
			return "", &missingAggregationKeyError{Key: key}
		}
		parts = append(parts, fmt.Sprint(val))
	}
	return strings.Join(parts, "|"), nil
}

func buildAggregatedEvent(key string, record aggregateRecord) *event.Event {
	fields := cloneFields(record.Fields)
	fields["aggregation_key"] = key
	fields["first_seen_unix"] = record.FirstSeen.UnixMilli()
	fields["last_seen_unix"] = record.LastSeen.UnixMilli()

	return &event.Event{
		ReceivedAt: time.Now(),
		Source: event.SourceMetadata{
			Type: "aggregated_flow",
		},
		Fields: fields,
	}
}

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

func sumValueOf(val any) int64 {
	switch typed := val.(type) {
	case sumValue:
		return int64FromAny(typed.Value)
	default:
		return int64FromAny(val)
	}
}

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

func minTimestamp(a, b int64) int64 {
	if a == 0 {
		return b
	}
	if b == 0 || a <= b {
		return a
	}
	return b
}

func maxTimestamp(a, b int64) int64 {
	if a == 0 {
		return b
	}
	if b == 0 || a >= b {
		return a
	}
	return b
}

func int64Field(fields map[string]any, key string) int64 {
	val, ok := fields[key]
	if !ok {
		return 0
	}
	return int64FromAny(val)
}

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

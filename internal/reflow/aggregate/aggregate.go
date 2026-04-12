package aggregate

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/flowstore"
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
}

type missingAggregationKeyError struct {
	Key string
}

func (e *missingAggregationKeyError) Error() string {
	return fmt.Sprintf("missing aggregation key field %q", e.Key)
}

func (r *aggregateRecord) Add(delta aggregateRecord, existed bool) error {
	if !existed {
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
	return nil
}

// Stateful keeps aggregate state and optionally expires keyed buckets.
type Stateful struct {
	cfg     config.AggregatorConfig
	emitted chan *event.Event

	store *flowstore.Store[string, aggregateRecord]

	mu    sync.Mutex
	state map[string]aggregateRecord
}

func NewStateful(cfg config.AggregatorConfig) *Stateful {
	a := &Stateful{
		cfg:     cfg,
		emitted: make(chan *event.Event, 1024),
	}
	if cfg.ResetInterval > 0 {
		a.store = flowstore.NewStore[string, aggregateRecord](
			flowstore.WithDefaultTTL[string, aggregateRecord](time.Duration(cfg.ResetInterval)*time.Millisecond),
			flowstore.WithRefreshTTLOnWrite[string, aggregateRecord](),
			flowstore.WithHooks[string, aggregateRecord](flowstore.Hooks[string, aggregateRecord]{
				OnDelete: func(key string, value aggregateRecord, reason flowstore.DeleteReason) {
					if reason != flowstore.DeleteReasonExpired && reason != flowstore.DeleteReasonFlushed {
						return
					}
					select {
					case a.emitted <- buildAggregatedEvent(key, value):
					default:
					}
				},
			}),
		)
		a.store.Start(a.sweeperInterval())
		return a
	}

	a.state = make(map[string]aggregateRecord)
	return a
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
				Stream:         "flow_data",
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
	if a.store != nil {
		if err := a.store.Add(key, record); err != nil {
			return nil, fmt.Errorf("flowstore add %q: %w", key, err)
		}
		return a.drainEmitted(), nil
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
	if cfg.TemplateID != 0 {
		appendField("template_id")
	}
	return out
}

func (a *Stateful) Flush() ([]*event.Event, error) {
	if a.store != nil {
		return a.drainEmitted(), nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]*event.Event, 0, len(a.state))
	for key, record := range a.state {
		out = append(out, buildAggregatedEvent(key, record))
	}
	return out, nil
}

func (a *Stateful) Close() ([]*event.Event, error) {
	if a.store != nil {
		a.store.Close()
		return a.drainEmitted(), nil
	}
	return a.Flush()
}

func (a *Stateful) Interval() time.Duration {
	if !a.cfg.Enabled || a.cfg.PeriodicInterval <= 0 {
		return 0
	}
	return time.Duration(a.cfg.PeriodicInterval) * time.Millisecond
}

func (a *Stateful) sweeperInterval() time.Duration {
	interval := time.Duration(a.cfg.ResetInterval) * time.Millisecond / 2
	if interval <= 0 {
		return time.Second
	}
	return interval
}

func (a *Stateful) drainEmitted() []*event.Event {
	var out []*event.Event
	for {
		select {
		case evt := <-a.emitted:
			out = append(out, evt)
		default:
			return out
		}
	}
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

	now := evt.ReceivedAt
	if now.IsZero() {
		now = time.Now()
	}

	recordFields := make(map[string]any)
	for key, val := range cfg.StaticFields {
		recordFields[key] = val
	}
	if cfg.TemplateID != 0 {
		recordFields["template_id"] = cfg.TemplateID
	}
	for _, keyField := range cfg.KeyFields {
		if val, ok := fields[keyField]; ok {
			recordFields[keyField] = val
		}
	}
	for _, sumField := range cfg.Sum {
		recordFields[sumField] = int64Field(fields, sumField)
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
		case firstValue:
			if _, exists := dst[key]; !exists {
				dst[key] = incoming
			}
		case currentValue:
			dst[key] = incoming
		default:
			if existing, ok := dst[key]; ok {
				switch lhs := existing.(type) {
				case int64:
					dst[key] = lhs + int64FromAny(val)
					continue
				case uint32:
					dst[key] = uint32(uint64(lhs) + uint64(uint32FromAny(val)))
					continue
				}
			}
			dst[key] = val
		}
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

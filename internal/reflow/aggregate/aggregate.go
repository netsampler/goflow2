package aggregate

import (
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
	Process(evt *event.Event) ([]*event.Event, error)
	Flush() ([]*event.Event, error)
	Close() ([]*event.Event, error)
	Interval() time.Duration
}

// New builds the configured aggregation stage. "none" keeps the input stream unchanged.
func New(cfg config.AggregatorConfig) (Aggregator, error) {
	switch cfg.Type {
	case "", "none":
		return passthrough{}, nil
	case "window":
		return NewWindow(cfg), nil
	case "periodic":
		return NewPeriodic(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported aggregator.type %q", cfg.Type)
	}
}

type passthrough struct{}

func (passthrough) Process(evt *event.Event) ([]*event.Event, error) { return []*event.Event{evt}, nil }
func (passthrough) Flush() ([]*event.Event, error)                   { return nil, nil }
func (passthrough) Close() ([]*event.Event, error)                   { return nil, nil }
func (passthrough) Interval() time.Duration                          { return 0 }

type aggregateRecord struct {
	Fields    map[string]any
	FirstSeen time.Time
	LastSeen  time.Time
}

// Add merges event deltas into the stored aggregate value.
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

// Window aggregates events until they expire out of FlowStore.
type Window struct {
	cfg     config.AggregatorConfig
	store   *flowstore.Store[string, aggregateRecord]
	emitted chan *event.Event
}

// NewWindow creates a TTL-backed aggregator keyed by configured fields.
func NewWindow(cfg config.AggregatorConfig) *Window {
	a := &Window{
		cfg:     cfg,
		emitted: make(chan *event.Event, 1024),
	}
	a.store = flowstore.NewStore[string, aggregateRecord](
		flowstore.WithDefaultTTL[string, aggregateRecord](time.Duration(cfg.FlushInterval)*time.Millisecond),
		flowstore.WithRefreshTTLOnWrite[string, aggregateRecord](),
		flowstore.WithHooks[string, aggregateRecord](flowstore.Hooks[string, aggregateRecord]{
			OnDelete: func(key string, value aggregateRecord, reason flowstore.DeleteReason) {
				if reason != flowstore.DeleteReasonExpired && reason != flowstore.DeleteReasonFlushed {
					return
				}
				select {
				case a.emitted <- buildAggregatedEvent("window", key, value):
				default:
				}
			},
		}),
	)
	a.store.Start(a.Interval())
	return a
}

func (a *Window) Process(evt *event.Event) ([]*event.Event, error) {
	key, record, err := aggregateFromEvent(a.cfg, evt)
	if err != nil {
		return nil, err
	}
	if err := a.store.Add(key, record); err != nil {
		return nil, fmt.Errorf("flowstore add %q: %w", key, err)
	}
	return a.Flush()
}

func (a *Window) Flush() ([]*event.Event, error) {
	var out []*event.Event
	for {
		select {
		case evt := <-a.emitted:
			out = append(out, evt)
		default:
			return out, nil
		}
	}
}

func (a *Window) Close() ([]*event.Event, error) {
	a.store.Close()
	return a.Flush()
}

func (a *Window) Interval() time.Duration {
	interval := time.Duration(a.cfg.FlushInterval) * time.Millisecond / 2
	if interval <= 0 {
		return time.Second
	}
	return interval
}

// Periodic keeps aggregate state and emits snapshots at a fixed interval without expiring buckets.
type Periodic struct {
	cfg   config.AggregatorConfig
	mu    sync.Mutex
	state map[string]aggregateRecord
}

func NewPeriodic(cfg config.AggregatorConfig) *Periodic {
	return &Periodic{
		cfg:   cfg,
		state: make(map[string]aggregateRecord),
	}
}

func (a *Periodic) Process(evt *event.Event) ([]*event.Event, error) {
	key, record, err := aggregateFromEvent(a.cfg, evt)
	if err != nil {
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

func (a *Periodic) Flush() ([]*event.Event, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]*event.Event, 0, len(a.state))
	for key, record := range a.state {
		out = append(out, buildAggregatedEvent("periodic", key, record))
	}
	return out, nil
}

func (a *Periodic) Close() ([]*event.Event, error) {
	return a.Flush()
}

func (a *Periodic) Interval() time.Duration {
	interval := time.Duration(a.cfg.PeriodicInterval) * time.Millisecond
	if interval <= 0 {
		return 30 * time.Second
	}
	return interval
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

	return key, aggregateRecord{
		Fields:    recordFields,
		FirstSeen: now,
		LastSeen:  now,
	}, nil
}

func buildKey(fields map[string]any, keyFields []string) (string, error) {
	if len(keyFields) == 0 {
		return "__all__", nil
	}
	parts := make([]string, 0, len(keyFields))
	for _, key := range keyFields {
		val, ok := fields[key]
		if !ok {
			return "", fmt.Errorf("missing aggregation key field %q", key)
		}
		parts = append(parts, fmt.Sprint(val))
	}
	return strings.Join(parts, "|"), nil
}

func buildAggregatedEvent(kind, key string, record aggregateRecord) *event.Event {
	fields := cloneFields(record.Fields)
	fields["aggregation_type"] = kind
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
	out := make(map[string]any, len(in)+4)
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

type firstValue struct {
	Value any
}

type currentValue struct {
	Value any
}

func mergeFields(dst, src map[string]any) {
	for key, val := range src {
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

func int64Field(fields map[string]any, key string) int64 {
	val, ok := fields[key]
	if !ok {
		return 0
	}
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

package aggregate

import (
	"fmt"
	"strconv"
	"strings"
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
	case "flowstore_window":
		return NewFlowStoreWindow(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported aggregator.type %q", cfg.Type)
	}
}

type passthrough struct{}

func (passthrough) Process(evt *event.Event) ([]*event.Event, error) {
	return []*event.Event{evt}, nil
}

func (passthrough) Flush() ([]*event.Event, error) {
	return nil, nil
}

func (passthrough) Close() ([]*event.Event, error) {
	return nil, nil
}

func (passthrough) Interval() time.Duration {
	return 0
}

// FlowRecord is the stored value for one active aggregation bucket.
type FlowRecord struct {
	AgentIP    string
	SubAgentID uint32
	SourceID   uint32
	SrcAddr    string
	DstAddr    string
	Proto      uint32
	SrcPort    uint32
	DstPort    uint32
	Bytes      int64
	Packets    int64
	FirstSeen  time.Time
	LastSeen   time.Time
}

// Add merges packet deltas into the stored flow value.
func (r *FlowRecord) Add(delta FlowRecord, existed bool) error {
	if !existed {
		*r = delta
		return nil
	}
	r.Bytes += delta.Bytes
	r.Packets += delta.Packets
	if delta.LastSeen.After(r.LastSeen) {
		r.LastSeen = delta.LastSeen
	}
	return nil
}

// FlowStoreWindow aggregates events until they expire out of FlowStore.
type FlowStoreWindow struct {
	cfg     config.AggregatorConfig
	store   *flowstore.Store[string, FlowRecord]
	emitted chan *event.Event
}

// NewFlowStoreWindow creates a TTL-backed flow aggregator keyed by configured fields.
func NewFlowStoreWindow(cfg config.AggregatorConfig) *FlowStoreWindow {
	a := &FlowStoreWindow{
		cfg:     cfg,
		emitted: make(chan *event.Event, 1024),
	}
	a.store = flowstore.NewStore[string, FlowRecord](
		flowstore.WithDefaultTTL[string, FlowRecord](time.Duration(cfg.FlushInterval)*time.Millisecond),
		flowstore.WithRefreshTTLOnWrite[string, FlowRecord](),
		flowstore.WithHooks[string, FlowRecord](flowstore.Hooks[string, FlowRecord]{
			OnDelete: func(key string, value FlowRecord, reason flowstore.DeleteReason) {
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
	a.store.Start(a.Interval())
	return a
}

// Process updates the active flow bucket for this event.
func (a *FlowStoreWindow) Process(evt *event.Event) ([]*event.Event, error) {
	key, record, err := a.recordFromEvent(evt)
	if err != nil {
		return nil, err
	}
	if err := a.store.Add(key, record); err != nil {
		return nil, fmt.Errorf("flowstore add %q: %w", key, err)
	}
	return a.Flush()
}

// Flush drains any expiry-driven records that have already been emitted by FlowStore hooks.
func (a *FlowStoreWindow) Flush() ([]*event.Event, error) {
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

// Close flushes the store so remaining active records are emitted before shutdown.
func (a *FlowStoreWindow) Close() ([]*event.Event, error) {
	a.store.Close()
	return a.Flush()
}

// Interval returns the sweeper cadence used to turn expired buckets into output events.
func (a *FlowStoreWindow) Interval() time.Duration {
	interval := time.Duration(a.cfg.FlushInterval) * time.Millisecond / 2
	if interval <= 0 {
		return time.Second
	}
	return interval
}

func (a *FlowStoreWindow) recordFromEvent(evt *event.Event) (string, FlowRecord, error) {
	fields := evt.Fields
	if fields == nil {
		return "", FlowRecord{}, fmt.Errorf("event fields are empty")
	}

	key, err := buildKey(fields, a.cfg.KeyFields)
	if err != nil {
		return "", FlowRecord{}, err
	}

	now := evt.ReceivedAt
	if now.IsZero() {
		now = time.Now()
	}

	return key, FlowRecord{
		AgentIP:    stringOrZero(fields, "agent_ip"),
		SubAgentID: uint32Field(fields, "sub_agent_id"),
		SourceID:   uint32Field(fields, "source_id"),
		SrcAddr:    stringOrZero(fields, "src_addr"),
		DstAddr:    stringOrZero(fields, "dst_addr"),
		Proto:      uint32Field(fields, "proto"),
		SrcPort:    uint32Field(fields, "src_port"),
		DstPort:    uint32Field(fields, "dst_port"),
		Bytes:      int64Field(fields, "bytes"),
		Packets:    int64Field(fields, "packets"),
		FirstSeen:  now,
		LastSeen:   now,
	}, nil
}

func buildKey(fields map[string]any, keyFields []string) (string, error) {
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

func buildAggregatedEvent(key string, record FlowRecord) *event.Event {
	return &event.Event{
		ReceivedAt: time.Now(),
		Source: event.SourceMetadata{
			Type: "aggregated_flow",
		},
		Fields: map[string]any{
			"aggregation_key": key,
			"agent_ip":        record.AgentIP,
			"sub_agent_id":    record.SubAgentID,
			"source_id":       record.SourceID,
			"src_addr":        record.SrcAddr,
			"dst_addr":        record.DstAddr,
			"proto":           record.Proto,
			"src_port":        record.SrcPort,
			"dst_port":        record.DstPort,
			"bytes":           record.Bytes,
			"packets":         record.Packets,
			"first_seen_unix": record.FirstSeen.UnixMilli(),
			"last_seen_unix":  record.LastSeen.UnixMilli(),
		},
	}
}

func stringOrZero(fields map[string]any, key string) string {
	val, ok := fields[key]
	if !ok {
		return ""
	}
	s, _ := val.(string)
	return s
}

func uint32Field(fields map[string]any, key string) uint32 {
	val, ok := fields[key]
	if !ok {
		return 0
	}
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
	case int:
		return int64(v)
	case uint32:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}

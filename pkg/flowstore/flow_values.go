package flowstore

import "time"

// FlowCounters carries optional deltas for packets/bytes and is intended to be
// embedded into richer structs when using Store.Add.
type FlowCounters struct {
	Bytes   *int64
	Packets *int64
}

// Add merges delta counters into the receiver.
func (c *FlowCounters) Add(delta FlowCounters, _ bool) error {
	if delta.Bytes != nil {
		if c.Bytes == nil {
			c.Bytes = new(int64)
		}
		*c.Bytes += *delta.Bytes
	}
	if delta.Packets != nil {
		if c.Packets == nil {
			c.Packets = new(int64)
		}
		*c.Packets += *delta.Packets
	}
	return nil
}

// Set copies counter values from the provided value.
func (c *FlowCounters) Set(val FlowCounters, _ bool) error {
	if val.Bytes != nil {
		c.Bytes = new(int64)
		*c.Bytes = *val.Bytes
	} else {
		c.Bytes = nil
	}
	if val.Packets != nil {
		c.Packets = new(int64)
		*c.Packets = *val.Packets
	} else {
		c.Packets = nil
	}
	return nil
}

// CopyFrom duplicates counters from the source value.
func (c *FlowCounters) CopyFrom(src FlowCounters) {
	_ = c.Set(src, true)
}

// FlowTimestamp carries flow start/end times and supports store merging.
type FlowTimestamp struct {
	Start time.Time
	End   time.Time
}

// Add updates the end time when applying a delta.
func (t *FlowTimestamp) Add(delta FlowTimestamp, _ bool) error {
	if delta.End.After(t.End) {
		t.End = delta.End
	}
	return nil
}

// Set initializes start on first insert and always updates end.
func (t *FlowTimestamp) Set(val FlowTimestamp, existed bool) error {
	if !existed {
		t.Start = val.Start
	}
	t.End = val.End
	return nil
}

// CopyFrom copies timestamps from src.
func (t *FlowTimestamp) CopyFrom(src FlowTimestamp) {
	t.Start = src.Start
	t.End = src.End
}

// Interface guards are compile-time assertions that these types satisfy the
// FlowStore value contracts expected by Add, Set, and copy operations.
var (
	_ Addable[FlowCounters]   = (*FlowCounters)(nil)
	_ Settable[FlowCounters]  = (*FlowCounters)(nil)
	_ Copyable[FlowCounters]  = (*FlowCounters)(nil)
	_ Addable[FlowTimestamp]  = (*FlowTimestamp)(nil)
	_ Settable[FlowTimestamp] = (*FlowTimestamp)(nil)
	_ Copyable[FlowTimestamp] = (*FlowTimestamp)(nil)
)

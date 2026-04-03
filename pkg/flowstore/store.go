package flowstore

import (
	"container/list"
	"errors"
	"sync"
	"time"
)

// ErrAddNotSupported is returned when Add is called on a value without Add support.
var ErrAddNotSupported = errors.New("flowstore: add operation not supported")

// Addable is implemented by values that can merge a delta into themselves.
// existed is true when the key was already present in the store.
type Addable[V any] interface {
	Add(delta V, existed bool) error
}

// Settable is implemented by values that can merge another value into themselves.
// existed is true when the key was already present in the store.
type Settable[V any] interface {
	Set(val V, existed bool) error
}

// Copyable is implemented by values that can copy data from another value.
type Copyable[V any] interface {
	CopyFrom(src V)
}

// Hooks receives store events.
type Hooks[K comparable, V any] struct {
	// OnSetMutate runs while the store lock is held after Set/Add mutates a value.
	// It can modify the stored value. existed indicates update vs insert.
	OnSetMutate func(key K, value *V, existed bool)
	// OnSet is called after Set/Add for a key; existed indicates update vs insert.
	OnSet func(key K, value V, existed bool)
	// OnDelete is called after Delete or expiry/eviction/flush; reason explains why.
	OnDelete func(key K, value V, reason DeleteReason)
	// OnGet is called after Get (not GetQuiet).
	OnGet func(key K, value V)
}

// DeleteReason explains why an OnDelete hook fired.
type DeleteReason int

const (
	DeleteReasonExplicit DeleteReason = iota // Delete method called
	DeleteReasonExpired                      // TTL expiry
	DeleteReasonEvicted                      // Max-size eviction
	DeleteReasonFlushed                      // Explicit flush of the store
)

// ExpireHook runs when a key is expired by TTL checks.
// It must not call back into the store to avoid deadlocks.
type ExpireHook[K comparable, V any] func(key K, val V) (extend bool, ttl time.Duration)

type StoreOption[K comparable, V any] func(*Store[K, V])

// WithDefaultTTL sets the store default TTL for new entries. A zero duration disables expiry.
func WithDefaultTTL[K comparable, V any](ttl time.Duration) StoreOption[K, V] {
	return func(s *Store[K, V]) {
		s.defaultTTL = ttl
	}
}

// WithRefreshTTLOnWrite refreshes the TTL on Add/Set when using the default TTL.
func WithRefreshTTLOnWrite[K comparable, V any]() StoreOption[K, V] {
	return func(s *Store[K, V]) {
		s.refreshTTL = true
	}
}

// WithRefreshTTLOnRead refreshes the TTL on Get when using the default TTL.
func WithRefreshTTLOnRead[K comparable, V any]() StoreOption[K, V] {
	return func(s *Store[K, V]) {
		s.refreshTTLOnRead = true
	}
}

// WithChangeNotifier sets a channel that receives a signal on store changes.
func WithChangeNotifier[K comparable, V any](ch chan struct{}) StoreOption[K, V] {
	return func(s *Store[K, V]) {
		s.changeNotify = ch
	}
}

// WithHooks sets store event hooks.
func WithHooks[K comparable, V any](hooks Hooks[K, V]) StoreOption[K, V] {
	return func(s *Store[K, V]) {
		s.hooks = hooks
	}
}

// WithMaxSize sets the maximum number of entries kept in the store.
func WithMaxSize[K comparable, V any](max int) StoreOption[K, V] {
	return func(s *Store[K, V]) {
		s.maxSize = max
	}
}

// WithExpireHook sets the hook called on TTL expiry.
func WithExpireHook[K comparable, V any](hook ExpireHook[K, V]) StoreOption[K, V] {
	return func(s *Store[K, V]) {
		s.onExpire = hook
	}
}

// WithNow overrides the time source, useful for tests.
func WithNow[K comparable, V any](now func() time.Time) StoreOption[K, V] {
	return func(s *Store[K, V]) {
		s.now = now
	}
}

type EntryOption func(*entryOptions)

type entryOptions struct {
	ttlSet       bool
	ttl          time.Duration
	noExpiration bool
}

// WithTTL overrides the entry TTL. A non-positive duration disables expiry.
func WithTTL(ttl time.Duration) EntryOption {
	return func(o *entryOptions) {
		o.ttlSet = true
		o.ttl = ttl
	}
}

// WithoutExpiration disables expiry for the entry.
func WithoutExpiration() EntryOption {
	return func(o *entryOptions) {
		o.ttlSet = true
		o.noExpiration = true
		o.ttl = 0
	}
}

type entry[K comparable, V any] struct {
	key       K
	value     V
	expiresAt time.Time
	elem      *list.Element // node in insertion order list for FIFO eviction
}

// Store is an in-memory KV store with TTL and FIFO eviction.
type Store[K comparable, V any] struct {
	mu               sync.RWMutex
	entries          map[K]*entry[K, V]
	order            *list.List
	onExpire         ExpireHook[K, V]
	defaultTTL       time.Duration
	refreshTTL       bool
	refreshTTLOnRead bool
	maxSize          int
	now              func() time.Time
	changeNotify     chan struct{}
	hooks            Hooks[K, V]
	stopSweeper      func()
}

// NewStore creates a FlowStore with the provided options.
func NewStore[K comparable, V any](opts ...StoreOption[K, V]) *Store[K, V] {
	s := &Store[K, V]{
		entries: make(map[K]*entry[K, V]),
		order:   list.New(),
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Add applies a delta to the value stored under key.
func (s *Store[K, V]) Add(key K, delta V, opts ...EntryOption) error {
	now := s.now()
	eopts := applyEntryOptions(opts) // capture per-call TTL options

	s.mu.Lock()
	events := make([]hookEvent[K, V], 0, 1)
	defer func() {
		s.mu.Unlock()
		s.fireHooks(events)
	}()

	// Existing entry: expire if needed, extend TTL if allowed, then apply delta.
	if ent, ok := s.entries[key]; ok {
		if s.expiredLocked(ent, now) && !s.maybeExtendLocked(ent, now) {
			s.recordDelete(&events, ent, DeleteReasonExpired)
			s.deleteLocked(ent)
			ent = nil
		}
		if ent != nil {
			if eopts.ttlSet {
				ent.expiresAt = s.expiresAtFor(now, eopts, true)
			} else if s.refreshTTL && s.defaultTTL > 0 {
				ent.expiresAt = now.Add(s.defaultTTL)
			}
			if err := addValue(&ent.value, delta, true); err != nil {
				return err
			}
			if s.hooks.OnSetMutate != nil {
				s.hooks.OnSetMutate(key, &ent.value, true)
			}
			s.notifyChangeLocked()
			s.recordSet(&events, key, ent.value, true)
			return nil
		}
	}

	// New entry: compute initial value, insert, apply TTL/max size, notify hooks.
	var val V
	if err := addValue(&val, delta, false); err != nil {
		return err
	}

	ent := s.insertLocked(key, val)
	if s.hooks.OnSetMutate != nil {
		s.hooks.OnSetMutate(key, &ent.value, false)
	}
	ent.expiresAt = s.expiresAtFor(now, eopts, false)
	s.enforceMaxSizeLocked(&events)
	s.notifyChangeLocked()
	s.recordSet(&events, key, val, false)
	return nil
}

// Set replaces or updates the value stored under key, returning whether the key existed.
func (s *Store[K, V]) Set(key K, val V, opts ...EntryOption) (bool, error) {
	now := s.now()
	eopts := applyEntryOptions(opts) // capture per-call TTL options

	s.mu.Lock()
	events := make([]hookEvent[K, V], 0, 1)
	defer func() {
		s.mu.Unlock()
		s.fireHooks(events)
	}()

	existed := false

	// Existing entry: expire if needed, then update value and TTL.
	if ent, ok := s.entries[key]; ok {
		if s.expiredLocked(ent, now) && !s.maybeExtendLocked(ent, now) {
			s.recordDelete(&events, ent, DeleteReasonExpired)
			s.deleteLocked(ent)
			ent = nil
		}
		if ent != nil {
			existed = true
			if eopts.ttlSet {
				ent.expiresAt = s.expiresAtFor(now, eopts, true)
			} else if s.refreshTTL && s.defaultTTL > 0 {
				ent.expiresAt = now.Add(s.defaultTTL)
			}
			if usedSet, err := setValue(&ent.value, val, true); err != nil {
				return existed, err
			} else if !usedSet {
				// Value does not support Set; replace wholesale.
				ent.value = val
			}
			if s.hooks.OnSetMutate != nil {
				s.hooks.OnSetMutate(key, &ent.value, true)
			}
			s.notifyChangeLocked()
			s.recordSet(&events, key, ent.value, true)
			return existed, nil
		}
	}

	// New entry: copy value if required, insert, enforce TTL/max size, notify hooks.
	var newVal V
	if usedSet, err := setValue(&newVal, val, false); err != nil {
		return existed, err
	} else if !usedSet {
		newVal = val
	}

	ent := s.insertLocked(key, newVal)
	if s.hooks.OnSetMutate != nil {
		s.hooks.OnSetMutate(key, &ent.value, false)
	}
	ent.expiresAt = s.expiresAtFor(now, eopts, false)
	s.enforceMaxSizeLocked(&events)
	s.notifyChangeLocked()
	s.recordSet(&events, key, newVal, false)
	return existed, nil
}

// Get copies the value into dest if present and not expired.
func (s *Store[K, V]) Get(key K, dest *V) bool {
	return s.get(key, dest, true)
}

// GetQuiet returns a value without firing hooks or refreshing TTL.
func (s *Store[K, V]) GetQuiet(key K, dest *V) bool {
	return s.get(key, dest, false)
}

func (s *Store[K, V]) get(key K, dest *V, withHooks bool) bool {
	now := s.now()

	s.mu.Lock()
	events := make([]hookEvent[K, V], 0, 1)
	defer func() {
		s.mu.Unlock()
		if withHooks {
			s.fireHooks(events)
		}
	}()

	ent, ok := s.entries[key]
	if !ok {
		return false
	}
	if s.expiredLocked(ent, now) {
		// Allow expire hook to refresh; otherwise drop the entry.
		if s.maybeExtendLocked(ent, now) {
			return s.copyValueLocked(dest, ent)
		}
		s.recordDelete(&events, ent, DeleteReasonExpired)
		s.deleteLocked(ent)
		s.notifyChangeLocked()
		return false
	}
	if withHooks && s.refreshTTLOnRead && s.defaultTTL > 0 {
		ent.expiresAt = now.Add(s.defaultTTL)
	}
	if withHooks {
		s.recordGet(&events, ent)
	}
	return s.copyValueLocked(dest, ent)
}

// Delete removes a key from the store.
func (s *Store[K, V]) Delete(key K) bool {
	s.mu.Lock()
	events := make([]hookEvent[K, V], 0, 1)
	defer func() {
		s.mu.Unlock()
		s.fireHooks(events)
	}()

	ent, ok := s.entries[key]
	if !ok {
		return false
	}
	// Always emit delete hook for explicit removals.
	s.recordDelete(&events, ent, DeleteReasonExplicit)
	s.deleteLocked(ent)
	s.notifyChangeLocked()
	return true
}

// Len returns the number of entries in the store.
func (s *Store[K, V]) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// Range iterates entries in FIFO order.
func (s *Store[K, V]) Range(fn func(key K, val V) bool) {
	now := s.now()

	s.mu.Lock()
	events := make([]hookEvent[K, V], 0, 1)
	s.expireBeforeLocked(now, &events) // prune stale entries before iterating
	items := make([]struct {
		key K
		val V
	}, 0, len(s.entries))
	for el := s.order.Front(); el != nil; el = el.Next() {
		ent := el.Value.(*entry[K, V])
		var v V
		copyValue(&v, ent.value)
		items = append(items, struct {
			key K
			val V
		}{key: ent.key, val: v})
	}
	s.mu.Unlock()
	s.fireHooks(events)

	for _, item := range items {
		if !fn(item.key, item.val) {
			return
		}
	}
}

// ExpireStale removes entries whose TTLs have elapsed.
func (s *Store[K, V]) ExpireStale() int {
	now := s.now()
	s.mu.Lock()
	events := make([]hookEvent[K, V], 0, 1)
	removed := s.expireBeforeLocked(now, &events)
	s.mu.Unlock()
	s.fireHooks(events)
	return removed
}

// Flush removes all entries from the store and emits delete hooks.
func (s *Store[K, V]) Flush() {
	s.mu.Lock()
	events := make([]hookEvent[K, V], 0, len(s.entries))
	for key, ent := range s.entries {
		_ = key // silence unused warning inside loop
		s.recordDelete(&events, ent, DeleteReasonFlushed)
		s.deleteLocked(ent)
	}
	s.notifyChangeLocked()
	s.mu.Unlock()
	s.fireHooks(events)
}

// StartSweeper begins periodic expiry checks. The returned function stops only
// the goroutine; it does not flush or otherwise mutate remaining live entries.
func (s *Store[K, V]) StartSweeper(interval time.Duration) func() {
	if interval <= 0 {
		return func() {}
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.ExpireStale()
			case <-stop:
				return
			}
		}
	}()
	return func() { close(stop) }
}

// Start installs the store-owned sweeper once. Additional calls are ignored.
func (s *Store[K, V]) Start(sweepInterval time.Duration) {
	if sweepInterval <= 0 {
		return
	}
	s.mu.Lock()
	if s.stopSweeper != nil {
		s.mu.Unlock()
		return
	}
	s.stopSweeper = s.StartSweeper(sweepInterval)
	s.mu.Unlock()
}

// Stop halts background tasks without removing entries or firing delete hooks.
func (s *Store[K, V]) Stop() {
	s.mu.Lock()
	stop := s.stopSweeper
	s.stopSweeper = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// Close stops background tasks and then flushes all remaining entries.
func (s *Store[K, V]) Close() {
	s.Stop()
	s.Flush()
}

func (s *Store[K, V]) copyValueLocked(dest *V, ent *entry[K, V]) bool {
	if dest == nil {
		return false
	}
	copyValue(dest, ent.value)
	return true
}

func addValue[V any](dst *V, delta V, existed bool) error {
	addable, ok := any(dst).(Addable[V])
	if !ok {
		return ErrAddNotSupported
	}
	return addable.Add(delta, existed)
}

func setValue[V any](dst *V, val V, existed bool) (usedSet bool, err error) {
	if dst == nil {
		return false, nil
	}
	if settable, ok := any(dst).(Settable[V]); ok {
		return true, settable.Set(val, existed)
	}
	*dst = val
	return false, nil
}

func copyValue[V any](dst *V, src V) {
	if dst == nil {
		return
	}
	if copyable, ok := any(dst).(Copyable[V]); ok {
		copyable.CopyFrom(src)
		return
	}
	if settable, ok := any(dst).(Settable[V]); ok {
		_ = settable.Set(src, true)
		return
	}
	*dst = src
}

func (s *Store[K, V]) expiresAtFor(now time.Time, opts entryOptions, existing bool) time.Time {
	if opts.noExpiration || (opts.ttlSet && opts.ttl <= 0) {
		return time.Time{}
	}
	if opts.ttlSet {
		return now.Add(opts.ttl)
	}
	if !existing && s.defaultTTL > 0 {
		return now.Add(s.defaultTTL)
	}
	return time.Time{}
}

func (s *Store[K, V]) insertLocked(key K, val V) *entry[K, V] {
	ent := &entry[K, V]{key: key, value: val}
	ent.elem = s.order.PushBack(ent)
	s.entries[key] = ent
	return ent
}

func (s *Store[K, V]) deleteLocked(ent *entry[K, V]) {
	delete(s.entries, ent.key)
	if ent.elem != nil {
		s.order.Remove(ent.elem)
		ent.elem = nil
	}
}

func (s *Store[K, V]) expiredLocked(ent *entry[K, V], now time.Time) bool {
	return !ent.expiresAt.IsZero() && !now.Before(ent.expiresAt)
}

func (s *Store[K, V]) maybeExtendLocked(ent *entry[K, V], now time.Time) bool {
	if s.onExpire == nil {
		return false
	}
	extend, ttl := s.onExpire(ent.key, ent.value)
	if !extend {
		return false
	}
	if ttl <= 0 {
		if s.defaultTTL <= 0 {
			ent.expiresAt = time.Time{}
			return true
		}
		ttl = s.defaultTTL
	}
	ent.expiresAt = now.Add(ttl)
	return true
}

func (s *Store[K, V]) expireBeforeLocked(now time.Time, events *[]hookEvent[K, V]) int {
	removed := 0
	for el := s.order.Front(); el != nil; {
		next := el.Next()
		ent := el.Value.(*entry[K, V])
		if s.expiredLocked(ent, now) {
			// Try extending via expire hook; otherwise drop and emit delete hook.
			if s.maybeExtendLocked(ent, now) {
				el = next
				continue
			}
			s.recordDelete(events, ent, DeleteReasonExpired)
			s.deleteLocked(ent)
			s.notifyChangeLocked()
			removed++
		}
		el = next
	}
	return removed
}

func (s *Store[K, V]) enforceMaxSizeLocked(events *[]hookEvent[K, V]) {
	if s.maxSize <= 0 {
		return
	}
	for len(s.entries) > s.maxSize {
		el := s.order.Front() // evict oldest (FIFO)
		if el == nil {
			return
		}
		ent := el.Value.(*entry[K, V])
		s.recordDelete(events, ent, DeleteReasonEvicted)
		s.deleteLocked(ent)
		s.notifyChangeLocked()
	}
}

func (s *Store[K, V]) notifyChangeLocked() {
	if s.changeNotify == nil {
		return
	}
	select {
	case s.changeNotify <- struct{}{}:
	default:
	}
}

// SetHooks replaces the store hooks at runtime.
func (s *Store[K, V]) SetHooks(hooks Hooks[K, V]) {
	s.mu.Lock()
	s.hooks = hooks
	s.mu.Unlock()
}

type hookEventType int

const (
	hookEventSet hookEventType = iota
	hookEventDelete
	hookEventGet
)

type hookEvent[K comparable, V any] struct {
	eventType hookEventType
	key       K
	value     V
	existed   bool // true when Set/Add updated an existing key
	reason    DeleteReason
}

func (s *Store[K, V]) recordSet(events *[]hookEvent[K, V], key K, value V, existed bool) {
	if s.hooks.OnSet == nil || events == nil {
		return
	}
	*events = append(*events, hookEvent[K, V]{eventType: hookEventSet, key: key, value: value, existed: existed})
}

func (s *Store[K, V]) recordDelete(events *[]hookEvent[K, V], ent *entry[K, V], reason DeleteReason) {
	if s.hooks.OnDelete == nil || events == nil || ent == nil {
		return
	}
	*events = append(*events, hookEvent[K, V]{eventType: hookEventDelete, key: ent.key, value: ent.value, reason: reason})
}

func (s *Store[K, V]) recordGet(events *[]hookEvent[K, V], ent *entry[K, V]) {
	if s.hooks.OnGet == nil || events == nil || ent == nil {
		return
	}
	*events = append(*events, hookEvent[K, V]{eventType: hookEventGet, key: ent.key, value: ent.value})
}

func (s *Store[K, V]) fireHooks(events []hookEvent[K, V]) {
	if len(events) == 0 {
		return
	}
	for _, event := range events {
		switch event.eventType {
		case hookEventSet:
			if s.hooks.OnSet != nil {
				s.hooks.OnSet(event.key, event.value, event.existed)
			}
		case hookEventDelete:
			if s.hooks.OnDelete != nil {
				s.hooks.OnDelete(event.key, event.value, event.reason)
			}
		case hookEventGet:
			if s.hooks.OnGet != nil {
				s.hooks.OnGet(event.key, event.value)
			}
		}
	}
}

func applyEntryOptions(opts []EntryOption) entryOptions {
	var eopts entryOptions
	for _, opt := range opts {
		opt(&eopts)
	}
	return eopts
}

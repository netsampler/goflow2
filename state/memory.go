package state

import (
	"sync"
)

type memoryState[K comparable, V any] struct {
	data map[K]V
	lock *sync.RWMutex
}

func (m *memoryState[K, V]) Close() error {
	return nil
}

func (m *memoryState[K, V]) Get(key K) (V, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	if v, ok := m.data[key]; ok {
		return v, nil
	} else {
		return v, ErrorKeyNotFound
	}
}

func (m *memoryState[K, V]) Add(key K, value V) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.data[key] = value
	return nil
}

func (m *memoryState[K, V]) Delete(key K) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	delete(m.data, key)
	return nil
}

func (m *memoryState[K, V]) Pop(key K) (V, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	if v, ok := m.data[key]; ok {
		delete(m.data, key)
		return v, nil
	} else {
		return v, ErrorKeyNotFound
	}
}

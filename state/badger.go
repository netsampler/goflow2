package state

import (
	"encoding/json"
	"github.com/dgraph-io/badger/v4"
	"net/url"
)

type badgerState[K comparable, V any] struct {
	memory    memoryState[K, V]
	urlParsed *url.URL
	db        *badger.DB
}

func (b *badgerState[K, V]) init() error {
	db, err := badger.Open(badger.DefaultOptions(b.urlParsed.Path))
	if err != nil {
		return err
	}
	b.db = db
	// pre-populate local memory copy from existing badger data
	err = b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			kRaw := item.Key()
			err := item.Value(func(vRaw []byte) error {
				var k K
				var v V
				if kErr := json.Unmarshal(kRaw, &k); kErr != nil {
					return kErr
				}
				if vErr := json.Unmarshal(vRaw, &v); vErr != nil {
					return vErr
				}
				return b.memory.Add(k, v)
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func (b *badgerState[K, V]) Close() error {
	return b.db.Close()
}

func (b *badgerState[K, V]) Get(key K) (V, error) {
	return b.memory.Get(key)
}

func (b *badgerState[K, V]) Add(key K, value V) error {
	return b.db.Update(func(txn *badger.Txn) error {
		k, err := json.Marshal(key)
		if err != nil {
			return err
		}
		v, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if err = txn.Set(k, v); err != nil {
			return err
		}
		if err = b.memory.Add(key, value); err != nil {
			return err
		}
		return nil
	})
}

func (b *badgerState[K, V]) Delete(key K) error {
	return b.db.Update(func(txn *badger.Txn) error {
		k, err := json.Marshal(key)
		if err != nil {
			return err
		}
		if err = txn.Delete(k); err != nil {
			return err
		}
		if err = b.memory.Delete(key); err != nil {
			return err
		}
		return nil
	})
}

func (b *badgerState[K, V]) Pop(key K) (V, error) {
	var v V
	err := b.db.Update(func(txn *badger.Txn) error {
		k, err := json.Marshal(key)
		if err != nil {
			return err
		}
		if err = txn.Delete(k); err != nil {
			return err
		}
		if v, err = b.memory.Pop(key); err != nil {
			return err
		}
		return nil
	})
	return v, err
}

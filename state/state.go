package state

import (
	"context"
	"fmt"
	"net/url"
	"sync"
)

var (
	SupportedSchemes = []string{"memory", "badger", "redis"}
	ErrorKeyNotFound = fmt.Errorf("key not found")
)

type State[K comparable, V any] interface {
	Close() error
	Get(key K) (V, error)
	Add(key K, value V) error
	Delete(key K) error
	Pop(key K) (V, error)
}

func NewState[K comparable, V any](rawUrl string) (State[K, V], error) {
	urlParsed, err := url.Parse(rawUrl)
	if err != nil {
		return nil, err
	}
	memory := memoryState[K, V]{
		data: make(map[K]V),
		lock: new(sync.RWMutex),
	}
	switch urlParsed.Scheme {
	case "memory":
		return &memory, nil
	case "badger":
		bd := &badgerState[K, V]{
			memory:    memory,
			urlParsed: urlParsed,
		}
		if err = bd.init(); err != nil {
			return nil, err
		} else {
			return bd, nil
		}
	case "redis", "rediss":
		ctx, cancel := context.WithCancel(context.Background())
		rd := &redisState[K, V]{
			memory:    memory,
			urlParsed: urlParsed,
			ctx:       ctx,
			cancel:    cancel,
			wg:        new(sync.WaitGroup),
		}
		if err = rd.init(); err != nil {
			return nil, err
		} else {
			return rd, nil
		}
	default:
		return nil, fmt.Errorf("unknown state name %s", urlParsed.Scheme)
	}
}

package state

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type redisState[K comparable, V any] struct {
	memory    memoryState[K, V]
	urlParsed *url.URL
	rPrefix   string
	db        *redis.Client
	ctx       context.Context
	cancel    context.CancelFunc
	wg        *sync.WaitGroup
}

const (
	redisOpAdd = 1
	redisOpDel = 2
)

func (r *redisState[K, V]) init() error {
	r.rPrefix = r.urlParsed.Query().Get("prefix")
	if r.rPrefix == "" {
		return fmt.Errorf("'prefix' name is required on redis state engine, place it on your URL query string")
	}
	opts, err := redis.ParseURL(r.urlParsed.String())
	if err != nil {
		return err
	}
	r.db = redis.NewClient(opts)
	// pre-populate local memory copy from existing redis data
	iter := r.db.Scan(r.ctx, 0, "*", 0).Iterator()
	for iter.Next(r.ctx) {
		kRaw := iter.Val()
		res := r.db.Get(r.ctx, kRaw)
		vRaw, err := res.Bytes()
		if err != nil {
			return err
		}
		var k K
		var v V
		kRaw, _ = strings.CutPrefix(kRaw, r.rPrefix)
		if err = json.Unmarshal([]byte(kRaw), &k); err != nil {
			return err
		}
		if err = json.Unmarshal(vRaw, &v); err != nil {
			return err
		}
		if err = r.memory.Add(k, v); err != nil {
			return err
		}
	}
	if err = iter.Err(); err != nil {
		return err
	}
	// subscribe to value changes
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ps := r.db.PSubscribe(r.ctx, fmt.Sprintf("%s*", r.rPrefix))
		defer ps.Close()
		ch := ps.Channel()
	mainLoop:
		for {
			select {
			case msgRaw := <-ch:
				op, err := strconv.Atoi(msgRaw.Payload)
				if err != nil {
					continue
				}
				keyRaw, _ := strings.CutPrefix(msgRaw.Channel, r.rPrefix)
				var k K
				if err = json.Unmarshal([]byte(keyRaw), &k); err != nil {
					continue
				}
				switch op {
				case redisOpAdd:
					cmd := r.db.Get(r.ctx, msgRaw.Channel)
					vBytes, err := cmd.Bytes()
					if err != nil {
						continue
					}
					var v V
					if err = json.Unmarshal(vBytes, &v); err != nil {
						continue
					}
					_ = r.memory.Add(k, v)
				case redisOpDel:
					_ = r.memory.Delete(k)
				}
			case <-r.ctx.Done():
				break mainLoop
			}
		}
	}()
	return nil
}

func (r *redisState[K, V]) Close() error {
	r.cancel()
	r.wg.Wait()
	return r.db.Close()
}

func (r *redisState[K, V]) Get(key K) (V, error) {
	return r.memory.Get(key)
}

func (r *redisState[K, V]) Add(key K, value V) error {
	k, err := json.Marshal(key)
	if err != nil {
		return err
	}
	v, err := json.Marshal(value)
	if err != nil {
		return err
	}
	kStr := fmt.Sprintf("%s%s", r.rPrefix, string(k))
	setStatus := r.db.Set(r.ctx, kStr, v, 0)
	if err = setStatus.Err(); err != nil {
		return err
	}
	pubStatus := r.db.Publish(r.ctx, kStr, redisOpAdd)
	if err = pubStatus.Err(); err != nil {
		return err
	}
	return nil
}

func (r *redisState[K, V]) Delete(key K) error {
	k, err := json.Marshal(key)
	if err != nil {
		return err
	}
	kStr := fmt.Sprintf("%s%s", r.rPrefix, string(k))
	delStatus := r.db.Del(r.ctx, kStr)
	if err = delStatus.Err(); err != nil {
		return err
	}
	pubStatus := r.db.Publish(r.ctx, kStr, redisOpDel)
	if err = pubStatus.Err(); err != nil {
		return err
	}
	return nil
}

func (r *redisState[K, V]) Pop(key K) (V, error) {
	v, err := r.Get(key)
	if err != nil {
		return v, err
	}
	err = r.Delete(key)
	return v, err
}

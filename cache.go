package main

import (
	"sync"
	"time"
)

// The old caches were bare maps that grew forever and never expired. That's
// mostly harmless for metadata, but actively broken for streams: debrid
// providers hand out time-limited URLs, so replaying a cached torrentio result
// an hour later gets you a link mpv can't open.

type cacheEntry[T any] struct {
	val T
	at  time.Time
}

type ttlCache[T any] struct {
	mu    sync.Mutex
	items map[string]cacheEntry[T]
	ttl   time.Duration
	max   int
}

func newCache[T any](ttl time.Duration, max int) *ttlCache[T] {
	return &ttlCache[T]{items: map[string]cacheEntry[T]{}, ttl: ttl, max: max}
}

func (c *ttlCache[T]) Get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok {
		var zero T
		return zero, false
	}
	if time.Since(e.at) > c.ttl {
		delete(c.items, key)
		var zero T
		return zero, false
	}
	return e.val, true
}

func (c *ttlCache[T]) Set(key string, val T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.max > 0 && len(c.items) >= c.max {
		// Cheap bound: drop anything already expired, and if that wasn't
		// enough, drop the oldest entry.
		var oldestKey string
		var oldestAt time.Time
		for k, e := range c.items {
			if time.Since(e.at) > c.ttl {
				delete(c.items, k)
				continue
			}
			if oldestAt.IsZero() || e.at.Before(oldestAt) {
				oldestKey, oldestAt = k, e.at
			}
		}
		if len(c.items) >= c.max && oldestKey != "" {
			delete(c.items, oldestKey)
		}
	}

	c.items[key] = cacheEntry[T]{val: val, at: time.Now()}
}

func (c *ttlCache[T]) Delete(key string) {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

func (c *ttlCache[T]) Clear() {
	c.mu.Lock()
	c.items = map[string]cacheEntry[T]{}
	c.mu.Unlock()
}

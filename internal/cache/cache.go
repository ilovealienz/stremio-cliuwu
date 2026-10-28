package cache

import (
	"strings"
	"sync"
	"time"
)

// The old caches were bare maps that grew forever and never expired. That's
// mostly harmless for metadata, but actively broken for streams: debrid
// providers hand out time-limited URLs, so replaying a cached torrentio result
// an hour later gets you a link mpv can't open.

type cacheEntry[T any] struct {
	val   T
	at    time.Time
	bytes int
}

type TTL[T any] struct {
	mu    sync.Mutex
	items map[string]cacheEntry[T]
	ttl   time.Duration
	max   int

	// Byte bound, for caches whose entries vary in size. A count bound
	// alone says nothing about memory when one entry can be a hundred times
	// another: forty posters is 1.6MB of thumbnails or 320MB of full-size
	// art, and the cache cannot tell the difference.
	//
	// Zero means no byte bound, which is right for entries that are all
	// roughly the same size.
	maxBytes int
	bytes    int
	sizeOf   func(T) int
}

func New[T any](ttl time.Duration, max int) *TTL[T] {
	return &TTL[T]{items: map[string]cacheEntry[T]{}, ttl: ttl, max: max}
}

// NewSized bounds by count and by total bytes, whichever binds first.
func NewSized[T any](ttl time.Duration, max, maxBytes int, sizeOf func(T) int) *TTL[T] {
	return &TTL[T]{
		items: map[string]cacheEntry[T]{}, ttl: ttl, max: max,
		maxBytes: maxBytes, sizeOf: sizeOf,
	}
}

// drop removes an entry and its byte count. Caller holds the lock.
func (c *TTL[T]) drop(key string) {
	if e, ok := c.items[key]; ok {
		c.bytes -= e.bytes
		delete(c.items, key)
	}
}

// oldest returns the least recently stored key, or "" when empty.
func (c *TTL[T]) oldest() string {
	var key string
	var at time.Time
	for k, e := range c.items {
		if at.IsZero() || e.at.Before(at) {
			key, at = k, e.at
		}
	}
	return key
}

func (c *TTL[T]) Get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok {
		var zero T
		return zero, false
	}
	if time.Since(e.at) > c.ttl {
		c.drop(key)
		var zero T
		return zero, false
	}
	return e.val, true
}

func (c *TTL[T]) Set(key string, val T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	size := 0
	if c.sizeOf != nil {
		size = c.sizeOf(val)
	}

	// One entry larger than the whole budget would evict everything else and
	// still not fit, so it is not worth storing at all.
	if c.maxBytes > 0 && size > c.maxBytes {
		c.drop(key)
		return
	}

	c.drop(key) // replacing: the old size no longer counts

	// Expired entries first — they are free to lose.
	for k, e := range c.items {
		if time.Since(e.at) > c.ttl {
			c.drop(k)
		}
	}

	// Then oldest-first until both bounds are satisfied.
	for (c.max > 0 && len(c.items) >= c.max) ||
		(c.maxBytes > 0 && c.bytes+size > c.maxBytes) {
		k := c.oldest()
		if k == "" {
			break
		}
		c.drop(k)
	}

	c.items[key] = cacheEntry[T]{val: val, at: time.Now(), bytes: size}
	c.bytes += size
}

func (c *TTL[T]) Delete(key string) {
	c.mu.Lock()
	c.drop(key)
	c.mu.Unlock()
}

// DeletePrefix drops every key starting with prefix.
func (c *TTL[T]) DeletePrefix(prefix string) {
	c.mu.Lock()
	for k := range c.items {
		if strings.HasPrefix(k, prefix) {
			c.drop(k)
		}
	}
	c.mu.Unlock()
}

func (c *TTL[T]) Clear() {
	c.mu.Lock()
	c.items = map[string]cacheEntry[T]{}
	c.bytes = 0
	c.mu.Unlock()
}

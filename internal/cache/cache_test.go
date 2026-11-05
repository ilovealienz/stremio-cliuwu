package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestExpiry(t *testing.T) {
	c := New[int](300*time.Millisecond, 0)
	c.Set("a", 1)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("fresh entry: %v %v", v, ok)
	}
	time.Sleep(400 * time.Millisecond)
	if _, ok := c.Get("a"); ok {
		t.Fatal("expired entry still returned")
	}
	if len(c.items) != 0 {
		t.Fatal("expired entry not dropped on read")
	}
}

func TestCountBoundEvictsOldest(t *testing.T) {
	c := New[int](time.Hour, 3)
	for i, k := range []string{"a", "b", "c"} {
		c.Set(k, i)
		time.Sleep(time.Millisecond) // distinct times, so "oldest" is defined
	}
	c.Set("d", 3)
	if _, ok := c.Get("a"); ok {
		t.Fatal("oldest entry kept past the bound")
	}
	for _, k := range []string{"b", "c", "d"} {
		if _, ok := c.Get(k); !ok {
			t.Fatalf("%s evicted instead of the oldest", k)
		}
	}
}

func TestReplaceDoesNotEvict(t *testing.T) {
	c := New[int](time.Hour, 2)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("a", 3) // same key: no room needed
	if v, _ := c.Get("a"); v != 3 {
		t.Fatalf("a = %d", v)
	}
	if _, ok := c.Get("b"); !ok {
		t.Fatal("replacing a key evicted another")
	}
}

func TestByteBound(t *testing.T) {
	c := NewSized[[]byte](time.Hour, 0, 100, func(b []byte) int { return len(b) })
	c.Set("a", make([]byte, 40))
	time.Sleep(time.Millisecond)
	c.Set("b", make([]byte, 40))
	time.Sleep(time.Millisecond)
	c.Set("c", make([]byte, 40)) // 120 > 100: a goes
	if _, ok := c.Get("a"); ok {
		t.Fatal("byte bound not enforced")
	}
	if c.bytes != 80 {
		t.Fatalf("bytes = %d, want 80", c.bytes)
	}

	c.Set("b", make([]byte, 10)) // replacing frees the old size
	if c.bytes != 50 {
		t.Fatalf("bytes after replace = %d, want 50", c.bytes)
	}

	// Bigger than the whole budget: not stored, and the old value under that
	// key is gone rather than left stale.
	c.Set("c", make([]byte, 200))
	if _, ok := c.Get("c"); ok {
		t.Fatal("oversized entry stored")
	}
	if _, ok := c.Get("b"); !ok {
		t.Fatal("oversized entry evicted everything else")
	}
	if c.bytes != 10 {
		t.Fatalf("bytes = %d, want 10", c.bytes)
	}
}

func TestDeletePrefixAndClear(t *testing.T) {
	c := NewSized[[]byte](time.Hour, 0, 1000, func(b []byte) int { return len(b) })
	c.Set("streams:tt1:1", make([]byte, 5))
	c.Set("streams:tt1:2", make([]byte, 5))
	c.Set("streams:tt2:1", make([]byte, 5))
	c.DeletePrefix("streams:tt1:")
	if _, ok := c.Get("streams:tt1:1"); ok {
		t.Fatal("prefix not deleted")
	}
	if _, ok := c.Get("streams:tt2:1"); !ok || c.bytes != 5 {
		t.Fatalf("wrong entries deleted, bytes %d", c.bytes)
	}
	c.Delete("streams:tt2:1")
	c.Delete("missing")
	if c.bytes != 0 {
		t.Fatalf("bytes after delete = %d", c.bytes)
	}
	c.Set("x", make([]byte, 7))
	c.Clear()
	if _, ok := c.Get("x"); ok || c.bytes != 0 {
		t.Fatal("clear left entries")
	}
}

// Run with -race.
func TestConcurrentUse(t *testing.T) {
	c := New[int](time.Hour, 50)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				k := fmt.Sprint(i % 80)
				c.Set(k, i)
				c.Get(k)
				if i%50 == 0 {
					c.DeletePrefix("1")
				}
			}
		}(g)
	}
	wg.Wait()
	if len(c.items) > 50 {
		t.Fatalf("%d entries past a bound of 50", len(c.items))
	}
}

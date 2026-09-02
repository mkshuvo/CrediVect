package cache

import (
	"sync"
	"time"
)

// DragonflyClient manages 24h idempotency keys and rate limiting.
// In local/test environments, uses an in-memory concurrent map with TTL expiration.
type DragonflyClient struct {
	mu     sync.RWMutex
	store  map[string]time.Time
}

func NewDragonflyClient() *DragonflyClient {
	c := &DragonflyClient{
		store: make(map[string]time.Time),
	}
	// Background garbage collector for expired keys
	go func() {
		for {
			time.Sleep(1 * time.Minute)
			c.mu.Lock()
			now := time.Now()
			for k, exp := range c.store {
				if now.After(exp) {
					delete(c.store, k)
				}
			}
			c.mu.Unlock()
		}
	}()
	return c
}

// SetNX simulates Redis/Dragonfly SET key value EX ttl NX (Set if Not eXists)
func (c *DragonflyClient) SetNX(key string, ttl time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if exp, exists := c.store[key]; exists && now.Before(exp) {
		return false // Key already exists and has not expired!
	}

	c.store[key] = now.Add(ttl)
	return true
}

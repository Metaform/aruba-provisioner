package status

import (
	"sync"
	"time"
)

// In memory cache for status responses
type statusCache struct {
	data     map[string]*cacheEntry
	mu       sync.RWMutex
	ttl      time.Duration
	stopChan chan struct{}
}

type cacheEntry struct {
	response  *ParticipantStatusResponse
	expiresAt time.Time
}

func newStatusCache(ttl time.Duration) *statusCache {
	cache := &statusCache{
		data:     make(map[string]*cacheEntry),
		ttl:      ttl,
		stopChan: make(chan struct{}),
	}

	go cache.cleanup()

	return cache
}

func (c *statusCache) get(key string) *ParticipantStatusResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.data[key]
	if !exists || time.Now().After(entry.expiresAt) {
		return nil
	}

	return entry.response
}

func (c *statusCache) set(key string, response *ParticipantStatusResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = &cacheEntry{
		response:  response,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *statusCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for key, entry := range c.data {
				if now.After(entry.expiresAt) {
					delete(c.data, key)
				}
			}
			c.mu.Unlock()
		case <-c.stopChan:
			// Graceful shutdown requested
			return
		}
	}
}

// Gracefully stops the cache cleanup goroutine
func (c *statusCache) stop() {
	close(c.stopChan)
}

// Removes a participant from the cache
func (c *statusCache) invalidate(participantName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, participantName)
}

func (c *statusCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]*cacheEntry)
}

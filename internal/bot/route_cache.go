package bot

import (
	"crypto/rand"
	"encoding/hex"
	"sync"

	"sushi-vesla-bot/internal/service"
)

type RouteCache struct {
	mu     sync.RWMutex
	routes map[string]service.Segment
}

func NewRouteCache() *RouteCache {
	return &RouteCache{
		routes: make(map[string]service.Segment),
	}
}

// Save сохраняет сегмент и возвращает ID
func (c *RouteCache) Save(segment service.Segment) string {
	id := generateID()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.routes[id] = segment
	return id
}

// Get возвращает сегмент по ID
func (c *RouteCache) Get(id string) (service.Segment, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	seg, ok := c.routes[id]
	return seg, ok
}

// Delete удаляет сегмент по ID
func (c *RouteCache) Delete(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.routes, id)
}

func generateID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

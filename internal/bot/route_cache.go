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

type SessionCache struct {
	mu       sync.RWMutex
	sessions map[string]*SessionData
}

type SessionData struct {
	Segments    []service.Segment
	CurrentPage int
	TotalPages  int
	TotalCount  int
}

var (
	routeCache   *RouteCache
	sessionCache *SessionCache
)

func init() {
	routeCache = &RouteCache{
		routes: make(map[string]service.Segment),
	}
	sessionCache = &SessionCache{
		sessions: make(map[string]*SessionData),
	}
}

func NewRouteCache() *RouteCache {
	return routeCache
}

func (c *RouteCache) Save(segment service.Segment) string {
	id := generateID()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.routes[id] = segment
	return id
}

func (c *RouteCache) Get(id string) (service.Segment, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	seg, ok := c.routes[id]
	return seg, ok
}

func (c *RouteCache) Delete(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.routes, id)
}

func (c *RouteCache) GetAll() map[string]service.Segment {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.routes
}

// Session methods
func (c *SessionCache) SaveSession(chatID int64, segments []service.Segment) string {
	sessionID := generateID()
	c.mu.Lock()
	defer c.mu.Unlock()

	maxSegments := 10
	if len(segments) > maxSegments {
		segments = segments[:maxSegments]
	}

	totalPages := (len(segments) + 2) / 3
	if totalPages == 0 {
		totalPages = 1
	}

	c.sessions[sessionID] = &SessionData{
		Segments:    segments,
		CurrentPage: 0,
		TotalPages:  totalPages,
		TotalCount:  len(segments),
	}
	return sessionID
}

func (c *SessionCache) GetSession(sessionID string) (*SessionData, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, ok := c.sessions[sessionID]
	return data, ok
}

func (c *SessionCache) GetPage(sessionID string, page int) ([]service.Segment, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, ok := c.sessions[sessionID]
	if !ok {
		return nil, false
	}

	start := page * 3
	end := start + 3
	if end > len(data.Segments) {
		end = len(data.Segments)
	}

	if start >= len(data.Segments) {
		return nil, false
	}

	return data.Segments[start:end], true
}

func (c *SessionCache) UpdatePage(sessionID string, page int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, ok := c.sessions[sessionID]
	if !ok {
		return false
	}

	if page < 0 || page >= data.TotalPages {
		return false
	}

	data.CurrentPage = page
	return true
}

func (c *SessionCache) DeleteSession(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, sessionID)
}

func generateID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

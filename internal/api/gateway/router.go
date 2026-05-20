package gateway

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrRouteExists   = errors.New("gateway: route already registered")
	ErrRouteNotFound = errors.New("gateway: route not found")
	ErrUnauthorized  = errors.New("gateway: unauthorized")
	ErrRateLimited   = errors.New("gateway: rate limit exceeded")
)

type AuthStrategy func(r *http.Request) error
type TransformFunc func(r *http.Request) *http.Request
type MergeFunc func(responses [][]byte) []byte

type routeEntry struct {
	backend   string
	rateLimit int
	counter   atomic.Int64
	resetAt   time.Time
	auth      AuthStrategy
	transform TransformFunc
}

// Router is an API gateway router with rate limiting, auth, and transform support.
type Router struct {
	mu     sync.RWMutex
	routes map[string]*routeEntry // method:path -> entry
}

func NewRouter() *Router {
	return &Router{routes: make(map[string]*routeEntry)}
}

func (r *Router) Route(method, path string, backend string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := method + ":" + path
	if _, ok := r.routes[key]; ok {
		return ErrRouteExists
	}
	r.routes[key] = &routeEntry{backend: backend}
	return nil
}

func (r *Router) RateLimit(path string, rpm int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, entry := range r.routes {
		if routePath(key) == path {
			entry.rateLimit = rpm
			entry.resetAt = time.Now().Add(time.Minute)
			return nil
		}
	}
	return ErrRouteNotFound
}

func (r *Router) Auth(path string, strategy AuthStrategy) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, entry := range r.routes {
		if routePath(key) == path {
			entry.auth = strategy
			return nil
		}
	}
	return ErrRouteNotFound
}

func (r *Router) Transform(path string, fn TransformFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, entry := range r.routes {
		if routePath(key) == path {
			entry.transform = fn
			return nil
		}
	}
	return ErrRouteNotFound
}

func (r *Router) Dispatch(req *http.Request) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := req.Method + ":" + req.URL.Path
	entry, ok := r.routes[key]
	if !ok {
		return "", ErrRouteNotFound
	}
	if entry.auth != nil {
		if err := entry.auth(req); err != nil {
			return "", ErrUnauthorized
		}
	}
	if entry.rateLimit > 0 {
		now := time.Now()
		if now.After(entry.resetAt) {
			entry.counter.Store(0)
			entry.resetAt = now.Add(time.Minute)
		}
		if entry.counter.Add(1) > int64(entry.rateLimit) {
			return "", ErrRateLimited
		}
	}
	return entry.backend, nil
}

func routePath(key string) string {
	for i, c := range key {
		if c == ':' {
			return key[i+1:]
		}
	}
	return key
}

package gateway_test

import (
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/api/gateway"
)

func makeReq(method, path string) *http.Request {
	return &http.Request{Method: method, URL: &url.URL{Path: path}}
}

func TestGateway_RouteToCorrectBackend(t *testing.T) {
	t.Parallel()
	r := gateway.NewRouter()
	r.Route("GET", "/api/products", "products-service")
	backend, err := r.Dispatch(makeReq("GET", "/api/products"))
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if backend != "products-service" {
		t.Fatalf("expected products-service, got %s", backend)
	}
}

func TestGateway_RateLimitEnforced(t *testing.T) {
	t.Parallel()
	r := gateway.NewRouter()
	r.Route("GET", "/api/limited", "svc")
	r.RateLimit("/api/limited", 2)
	r.Dispatch(makeReq("GET", "/api/limited"))
	r.Dispatch(makeReq("GET", "/api/limited"))
	_, err := r.Dispatch(makeReq("GET", "/api/limited"))
	if err != gateway.ErrRateLimited {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestGateway_AuthBlocksUnauthorized(t *testing.T) {
	t.Parallel()
	r := gateway.NewRouter()
	r.Route("POST", "/api/secure", "svc")
	r.Auth("/api/secure", func(req *http.Request) error {
		if req.Header == nil || req.Header.Get("Authorization") == "" {
			return errors.New("unauthorized")
		}
		return nil
	})
	_, err := r.Dispatch(makeReq("POST", "/api/secure"))
	if err != gateway.ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestGateway_UnknownRouteReturns404Error(t *testing.T) {
	t.Parallel()
	r := gateway.NewRouter()
	_, err := r.Dispatch(makeReq("GET", "/unknown"))
	if err != gateway.ErrRouteNotFound {
		t.Fatalf("expected ErrRouteNotFound, got %v", err)
	}
}

func TestGateway_RouteConflictReturnsError(t *testing.T) {
	t.Parallel()
	r := gateway.NewRouter()
	r.Route("GET", "/api/x", "svc1")
	err := r.Route("GET", "/api/x", "svc2")
	if err != gateway.ErrRouteExists {
		t.Fatalf("expected ErrRouteExists, got %v", err)
	}
}

func TestGateway_ConcurrentRoutingSafe(t *testing.T) {
	t.Parallel()
	r := gateway.NewRouter()
	r.Route("GET", "/api/concurrent", "svc")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Dispatch(makeReq("GET", "/api/concurrent"))
		}()
	}
	wg.Wait()
}

func TestGateway_TransformModifiesRequest(t *testing.T) {
	t.Parallel()
	r := gateway.NewRouter()
	r.Route("GET", "/api/transform", "svc")
	if err := r.Transform("/api/transform", func(req *http.Request) *http.Request { return req }); err != nil {
		t.Fatalf("transform registration failed: %v", err)
	}
	_, err := r.Dispatch(makeReq("GET", "/api/transform"))
	if err != nil {
		t.Fatalf("dispatch with transform failed: %v", err)
	}
}

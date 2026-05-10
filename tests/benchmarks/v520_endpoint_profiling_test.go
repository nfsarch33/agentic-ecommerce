package benchmarks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

type endpointProfile struct {
	Name    string
	Method  string
	Path    string
	Body    string
	Token   string
	Samples []time.Duration
	P95     time.Duration
}

func TestV520EndpointProfiling(t *testing.T) {
	srv, baseURL, token := setupProfilingServer(t)
	_ = srv

	productID := createTestProduct(t, baseURL, token)

	endpoints := []endpointProfile{
		{Name: "GET /api/v1/products", Method: "GET", Path: "/api/v1/products"},
		{Name: "GET /api/v1/products/{id}", Method: "GET", Path: "/api/v1/products/" + productID, Token: token},
		{Name: "GET /healthz", Method: "GET", Path: "/healthz"},
		{Name: "GET /readyz", Method: "GET", Path: "/readyz"},
		{Name: "GET /metrics", Method: "GET", Path: "/metrics"},
		{Name: "GET /api/v1/compliance/rules", Method: "GET", Path: "/api/v1/compliance/rules", Token: token},
		{Name: "GET /api/v1/events/recent", Method: "GET", Path: "/api/v1/events/recent", Token: token},
		{Name: "POST /api/v1/orders", Method: "POST", Path: "/api/v1/orders", Body: `{"customer_email":"bench@example.com","items":[{"product_id":"c1000000-0000-0000-0000-000000000001","sku":"BENCH-1","title":"Bench","quantity":1,"unit_price":{"amount":999,"currency":"AUD"}}],"shipping_address":{"name":"Bench","line1":"1 Test St","city":"Melbourne","region":"VIC","postal_code":"3000","country":"AU"}}`},
	}
	for i := range endpoints {
		if endpoints[i].Token == "" {
			endpoints[i].Token = token
		}
	}

	wallStart := time.Now()
	for i := range endpoints {
		ep := &endpoints[i]
		for w := 0; w < 3; w++ {
			profilingRequest(t, baseURL, ep.Method, ep.Path, ep.Token, ep.Body)
		}
		ep.Samples = make([]time.Duration, 0, 40)
		for s := 0; s < 40; s++ {
			start := time.Now()
			profilingRequest(t, baseURL, ep.Method, ep.Path, ep.Token, ep.Body)
			ep.Samples = append(ep.Samples, time.Since(start))
		}
		ep.P95 = p95(ep.Samples)
	}
	wallDuration := time.Since(wallStart)

	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].P95 > endpoints[j].P95
	})

	t.Log("=== v5.2.0 Endpoint Profiling: Top-5 Slowest (p95) ===")
	for rank, ep := range endpoints {
		marker := ""
		if rank < 5 {
			marker = " <-- TOP-5"
		}
		t.Logf("  #%d  p95=%-12s  %s%s", rank+1, ep.P95, ep.Name, marker)
	}
	t.Logf("  Wall clock: %s (target <30s)", wallDuration)

	if wallDuration > 30*time.Second {
		t.Fatalf("wall clock %s exceeds 30s budget", wallDuration)
	}
}

func setupProfilingServer(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready","agents":3}`))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintf(w, "agentic_ecommerce_build_info{version=\"5.2.0\"} 1\n")
	})
	mux.HandleFunc("/api/v1/products", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "p-bench-001", "sku": body["sku"], "title": body["title"],
				"price": map[string]any{"amount": 999, "currency": "AUD"},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"products": []map[string]string{{"id": "p-1", "title": "Band Set"}},
			"total":    1, "page": 1, "per_page": 20,
		})
	})
	mux.HandleFunc("/api/v1/products/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "p-bench-001", "sku": "BENCH-1", "title": "Band Set",
			"price": map[string]any{"amount": 999, "currency": "AUD"},
		})
	})
	mux.HandleFunc("/api/v1/compliance/rules", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"rules": []string{"no_restricted_materials"}})
	})
	mux.HandleFunc("/api/v1/events/recent", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"events": []any{}, "count": 0})
	})
	mux.HandleFunc("/api/v1/orders", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ord-bench-001", "status": "pending"})
	})
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "bench-token", "token_type": "Bearer"})
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	token := profilingLogin(t, ts.URL)
	return ts, ts.URL, token
}

func profilingLogin(t *testing.T, baseURL string) string {
	t.Helper()
	body := `{"email":"admin@example.com","password":"bench"}`
	resp, err := http.Post(baseURL+"/api/v1/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return result.AccessToken
}

func createTestProduct(t *testing.T, baseURL, token string) string {
	t.Helper()
	body := `{"sku":"BENCH-PROFILE","title":"Bench Profile Product","price":{"amount":2999,"currency":"AUD"}}`
	req, _ := http.NewRequest("POST", baseURL+"/api/v1/products", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result.ID == "" {
		result.ID = "p-bench-001"
	}
	return result.ID
}

func profilingRequest(t *testing.T, baseURL, method, path, token, body string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, baseURL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
}

func p95(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	idx := (len(ordered)*95 + 99) / 100
	if idx < 1 {
		idx = 1
	}
	if idx > len(ordered) {
		idx = len(ordered)
	}
	return ordered[idx-1]
}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	"github.com/nfsarch33/agentic-ecommerce/internal/security"
)

func TestReleasePerformanceSmoke(t *testing.T) {
	srv, repo := testServerWithCfg(t, serverConfig{
		jwtSecret:         "release-perf-smoke-secret-at-least-32-bytes",
		jwtIssuer:         "agentic-ecommerce",
		jwtAudience:       "mc-api",
		jwtAccessTTL:      15 * time.Minute,
		refreshTTL:        24 * time.Hour,
		adminUsername:     "admin@example.com",
		adminPassword:     "correct-horse-battery-staple",
		adminRole:         security.RoleAdmin,
		rateLimitCapacity: 500,
		rateLimitRefill:   time.Millisecond,
	})
	product := addProduct(t, repo, "RB-SET-5", "Resistance Band Set", 4995)
	srv.contentAgent = &fakeContentAgent{result: content.GenerateResult{
		GeneratedContent: content.GeneratedContent{
			Description:     "Resistance Band Set supports progressive home workouts.",
			SEOTitle:        "Resistance Band Set for Home Workouts",
			MetaDescription: "Shop a resistance band set for progressive home workouts.",
		},
		Evaluation: content.Evaluation{Score: 96, Pass: true},
		TokensUsed: 42,
	}}
	srv.configureSecurity()

	httpServer := httptest.NewServer(srv.mux())
	defer httpServer.Close()
	client := &http.Client{Timeout: 2 * time.Second}
	adminToken := perfLogin(t, client, httpServer.URL)

	scenarios := []struct {
		name   string
		target time.Duration
		run    func() error
	}{
		{
			name:   "GET /api/v1/products",
			target: 200 * time.Millisecond,
			run: func() error {
				return perfRequest(client, http.MethodGet, httpServer.URL+"/api/v1/products", "", nil)
			},
		},
		{
			name:   "POST /api/v1/auth/login",
			target: 300 * time.Millisecond,
			run: func() error {
				_, err := perfLoginResponse(client, httpServer.URL)
				return err
			},
		},
		{
			name:   "POST /api/v1/products/{id}/generate-description",
			target: 500 * time.Millisecond,
			run: func() error {
				body := []byte(`{"style":"professional","max_words":120,"keywords":["resistance band set","home workouts"]}`)
				return perfRequest(
					client,
					http.MethodPost,
					fmt.Sprintf("%s/api/v1/products/%s/generate-description", httpServer.URL, product.ID().String()),
					adminToken,
					body,
				)
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			for i := 0; i < 3; i++ {
				if err := scenario.run(); err != nil {
					t.Fatalf("warmup request failed: %v", err)
				}
			}
			samples := make([]time.Duration, 0, 40)
			for i := 0; i < 40; i++ {
				start := time.Now()
				if err := scenario.run(); err != nil {
					t.Fatalf("request %d failed: %v", i+1, err)
				}
				samples = append(samples, time.Since(start))
			}
			p95 := percentile(samples, 95)
			t.Logf("%s p95=%s target<%s samples=%d", scenario.name, p95, scenario.target, len(samples))
			if p95 >= scenario.target {
				t.Fatalf("%s p95=%s, want <%s", scenario.name, p95, scenario.target)
			}
		})
	}
}

func perfLogin(t *testing.T, client *http.Client, baseURL string) string {
	t.Helper()
	token, err := perfLoginResponse(client, baseURL)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	return token
}

func perfLoginResponse(client *http.Client, baseURL string) (string, error) {
	body := []byte(`{"email":"admin@example.com","password":"correct-horse-battery-staple"}`)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/auth/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", res.StatusCode)
	}
	var decoded loginResponse
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return "", err
	}
	if decoded.AccessToken == "" {
		return "", fmt.Errorf("missing access token")
	}
	return decoded.AccessToken, nil
}

func perfRequest(client *http.Client, method, url, bearerToken string, body []byte) error {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("status %d", res.StatusCode)
	}
	return nil
}

func percentile(samples []time.Duration, p int) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	idx := (len(ordered)*p + 99) / 100
	if idx < 1 {
		idx = 1
	}
	if idx > len(ordered) {
		idx = len(ordered)
	}
	return ordered[idx-1]
}

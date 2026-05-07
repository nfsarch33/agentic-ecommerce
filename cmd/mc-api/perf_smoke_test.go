package main

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

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
	"github.com/nfsarch33/agentic-ecommerce/internal/security"
	"github.com/nfsarch33/agentic-ecommerce/internal/webhook/outbound"
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
	srv.workflowClient = &fakeTemporalWorkflowClient{run: fakeWorkflowRun{id: "product-publish-" + product.ID().String(), runID: "run-perf-smoke"}}
	srv.mediaService = intelligence.NewService(intelligence.ServiceConfig{HTTPClient: mediaRoundTripClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(strings.NewReader(mediaOnePixelPNGString())),
		}, nil
	})})
	webhookReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer webhookReceiver.Close()
	srv.webhookService = outbound.NewService(outbound.ServiceConfig{
		Client: outbound.NewClient(outbound.ClientConfig{
			HTTPClient:  webhookReceiver.Client(),
			MaxAttempts: 1,
			Backoff:     func(int) time.Duration { return 0 },
		}),
	})
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
			target: 100 * time.Millisecond,
			run: func() error {
				return perfRequest(client, http.MethodGet, httpServer.URL+"/api/v1/products", "", nil)
			},
		},
		{
			name:   "POST /api/v1/orders",
			target: 200 * time.Millisecond,
			run: func() error {
				body := []byte(`{"customer_email":"shopper@example.com","items":[{"product_id":"c1000000-0000-0000-0000-000000000001","sku":"BAND-001","title":"Resistance Band","quantity":1,"unit_price":{"amount":2495,"currency":"AUD"}}],"shipping_address":{"name":"Jane Shopper","line1":"1 Market Street","city":"Sydney","region":"NSW","postal_code":"2000","country":"AU"}}`)
				return perfRequest(client, http.MethodPost, httpServer.URL+"/api/v1/orders", "", body)
			},
		},
		{
			name:   "POST /api/v1/products/{id}/generate-description",
			target: 2 * time.Second,
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
		{
			name:   "POST /api/v1/workflows/product-publish",
			target: 500 * time.Millisecond,
			run: func() error {
				body := []byte(`{"product_id":"` + product.ID().String() + `","requested_by":"perf-smoke"}`)
				return perfRequest(client, http.MethodPost, httpServer.URL+"/api/v1/workflows/product-publish", adminToken, body)
			},
		},
		{
			name:   "POST /api/v1/media/{id}/validate",
			target: 500 * time.Millisecond,
			run: func() error {
				sourceBody, err := perfRequestBody(
					client,
					http.MethodPost,
					httpServer.URL+"/api/v1/media/source",
					adminToken,
					[]byte(`{"url":"http://127.0.0.1:18081/fixtures/resistance-band.png","product_id":"`+product.ID().String()+`","alt_text":"Resistance band product image"}`),
				)
				if err != nil {
					return err
				}
				var sourced struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(sourceBody, &sourced); err != nil {
					return err
				}
				if sourced.ID == "" {
					return fmt.Errorf("missing sourced media id")
				}
				processBody, err := perfRequestBody(
					client,
					http.MethodPost,
					httpServer.URL+"/api/v1/media/process",
					adminToken,
					[]byte(`{"media_id":"`+sourced.ID+`","format":"image/webp"}`),
				)
				if err != nil {
					return err
				}
				var processed struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(processBody, &processed); err != nil {
					return err
				}
				if processed.ID == "" {
					return fmt.Errorf("missing processed media id")
				}
				return perfRequest(client, http.MethodPost, httpServer.URL+"/api/v1/media/"+processed.ID+"/validate", adminToken, nil)
			},
		},
		{
			name:   "POST /api/v1/webhooks/{id}/test",
			target: 500 * time.Millisecond,
			run: func() error {
				createBody, err := perfRequestBody(
					client,
					http.MethodPost,
					httpServer.URL+"/api/v1/webhooks",
					adminToken,
					[]byte(`{"url":"`+webhookReceiver.URL+`","event_types":["order.placed"],"secret":"perf-local-secret"}`),
				)
				if err != nil {
					return err
				}
				var created struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(createBody, &created); err != nil {
					return err
				}
				if created.ID == "" {
					return fmt.Errorf("missing webhook id")
				}
				if err := perfRequest(client, http.MethodPost, httpServer.URL+"/api/v1/webhooks/"+created.ID+"/test", adminToken, []byte(`{"event_type":"order.placed"}`)); err != nil {
					return err
				}
				return perfRequest(client, http.MethodDelete, httpServer.URL+"/api/v1/webhooks/"+created.ID, adminToken, nil)
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
	_, err := perfRequestBody(client, method, url, bearerToken, body)
	return err
}

func perfRequestBody(client *http.Client, method, url, bearerToken string, body []byte) ([]byte, error) {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	defer res.Body.Close()
	payload, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		return nil, readErr
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d body=%s", res.StatusCode, string(payload))
	}
	return payload, nil
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

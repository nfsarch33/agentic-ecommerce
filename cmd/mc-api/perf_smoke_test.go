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
	fixture := newReleasePerformanceFixture(t)
	for _, scenario := range releasePerfScenarios(fixture) {
		assertReleasePerfScenario(t, scenario)
	}
}

type releasePerfFixture struct {
	client             *http.Client
	baseURL            string
	adminToken         string
	productID          string
	webhookReceiverURL string
}

type releasePerfScenario struct {
	name   string
	target time.Duration
	run    func() error
}

func newReleasePerformanceFixture(t *testing.T) releasePerfFixture {
	t.Helper()

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
	t.Cleanup(webhookReceiver.Close)
	srv.webhookService = outbound.NewService(outbound.ServiceConfig{
		Client: outbound.NewClient(outbound.ClientConfig{
			HTTPClient:  webhookReceiver.Client(),
			MaxAttempts: 1,
			Backoff:     func(int) time.Duration { return 0 },
			SSRFGuard:   outbound.NewPermissiveSSRFGuard(),
		}),
	})
	srv.configureSecurity()

	httpServer := httptest.NewServer(srv.mux())
	t.Cleanup(httpServer.Close)
	client := &http.Client{Timeout: 2 * time.Second}
	return releasePerfFixture{
		client:             client,
		baseURL:            httpServer.URL,
		adminToken:         perfLogin(t, client, httpServer.URL),
		productID:          product.ID().String(),
		webhookReceiverURL: webhookReceiver.URL,
	}
}

func releasePerfScenarios(fixture releasePerfFixture) []releasePerfScenario {
	return []releasePerfScenario{
		{
			name:   "GET /api/v1/products",
			target: 100 * time.Millisecond,
			run: func() error {
				return fixture.listProducts()
			},
		},
		{
			name:   "POST /api/v1/orders",
			target: 200 * time.Millisecond,
			run: func() error {
				return fixture.createOrder()
			},
		},
		{
			name:   "POST /api/v1/products/{id}/generate-description",
			target: 2 * time.Second,
			run: func() error {
				return fixture.generateDescription()
			},
		},
		{
			name:   "POST /api/v1/workflows/product-publish",
			target: 500 * time.Millisecond,
			run: func() error {
				return fixture.startProductPublishWorkflow()
			},
		},
		{
			name:   "POST /api/v1/media/{id}/validate",
			target: 500 * time.Millisecond,
			run: func() error {
				return fixture.validateMedia()
			},
		},
		{
			name:   "POST /api/v1/webhooks/{id}/test",
			target: 500 * time.Millisecond,
			run: func() error {
				return fixture.testWebhookDelivery()
			},
		},
	}
}

func assertReleasePerfScenario(t *testing.T, scenario releasePerfScenario) {
	t.Helper()
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

func (f releasePerfFixture) listProducts() error {
	return perfRequest(f.client, http.MethodGet, f.baseURL+"/api/v1/products", "", nil)
}

func (f releasePerfFixture) createOrder() error {
	body := []byte(`{"customer_email":"shopper@example.com","items":[{"product_id":"c1000000-0000-0000-0000-000000000001","sku":"BAND-001","title":"Resistance Band","quantity":1,"unit_price":{"amount":2495,"currency":"AUD"}}],"shipping_address":{"name":"Jane Shopper","line1":"1 Market Street","city":"Sydney","region":"NSW","postal_code":"2000","country":"AU"}}`)
	return perfRequest(f.client, http.MethodPost, f.baseURL+"/api/v1/orders", "", body)
}

func (f releasePerfFixture) generateDescription() error {
	body := []byte(`{"style":"professional","max_words":120,"keywords":["resistance band set","home workouts"]}`)
	return perfRequest(
		f.client,
		http.MethodPost,
		fmt.Sprintf("%s/api/v1/products/%s/generate-description", f.baseURL, f.productID),
		f.adminToken,
		body,
	)
}

func (f releasePerfFixture) startProductPublishWorkflow() error {
	body := []byte(`{"product_id":"` + f.productID + `","requested_by":"perf-smoke"}`)
	return perfRequest(f.client, http.MethodPost, f.baseURL+"/api/v1/workflows/product-publish", f.adminToken, body)
}

func (f releasePerfFixture) validateMedia() error {
	sourcedID, err := f.sourceMedia()
	if err != nil {
		return err
	}
	processedID, err := f.processMedia(sourcedID)
	if err != nil {
		return err
	}
	return perfRequest(f.client, http.MethodPost, f.baseURL+"/api/v1/media/"+processedID+"/validate", f.adminToken, nil)
}

func (f releasePerfFixture) sourceMedia() (string, error) {
	body := []byte(`{"url":"http://127.0.0.1:18081/fixtures/resistance-band.png","product_id":"` + f.productID + `","alt_text":"Resistance band product image"}`)
	payload, err := perfRequestBody(f.client, http.MethodPost, f.baseURL+"/api/v1/media/source", f.adminToken, body)
	if err != nil {
		return "", err
	}
	return perfIDFromPayload(payload, "sourced media")
}

func (f releasePerfFixture) processMedia(mediaID string) (string, error) {
	body := []byte(`{"media_id":"` + mediaID + `","format":"image/webp"}`)
	payload, err := perfRequestBody(f.client, http.MethodPost, f.baseURL+"/api/v1/media/process", f.adminToken, body)
	if err != nil {
		return "", err
	}
	return perfIDFromPayload(payload, "processed media")
}

func (f releasePerfFixture) testWebhookDelivery() error {
	webhookID, err := f.createWebhook()
	if err != nil {
		return err
	}
	if err := perfRequest(f.client, http.MethodPost, f.baseURL+"/api/v1/webhooks/"+webhookID+"/test", f.adminToken, []byte(`{"event_type":"order.placed"}`)); err != nil {
		return err
	}
	return perfRequest(f.client, http.MethodDelete, f.baseURL+"/api/v1/webhooks/"+webhookID, f.adminToken, nil)
}

func (f releasePerfFixture) createWebhook() (string, error) {
	body := []byte(`{"url":"` + f.webhookReceiverURL + `","event_types":["order.placed"],"secret":"perf-local-secret"}`)
	payload, err := perfRequestBody(f.client, http.MethodPost, f.baseURL+"/api/v1/webhooks", f.adminToken, body)
	if err != nil {
		return "", err
	}
	return perfIDFromPayload(payload, "webhook")
}

func perfIDFromPayload(payload []byte, label string) (string, error) {
	var decoded struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", err
	}
	if decoded.ID == "" {
		return "", fmt.Errorf("missing %s id", label)
	}
	return decoded.ID, nil
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

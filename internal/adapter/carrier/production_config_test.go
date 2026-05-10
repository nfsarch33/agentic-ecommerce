package carrier_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/carrier"
)

func TestProductionAusPostConfigMissingKey(t *testing.T) {
	t.Setenv("EC_AUSPOST_API_KEY", "")
	t.Setenv("EC_AUSPOST_API_SECRET", "secret")
	_, err := carrier.ProductionAusPostConfig()
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if !errors.Is(err, carrier.ErrCarrierClientUnconfigured) {
		t.Fatalf("error = %v, want ErrCarrierClientUnconfigured", err)
	}
}

func TestProductionAusPostConfigMissingSecret(t *testing.T) {
	t.Setenv("EC_AUSPOST_API_KEY", "key")
	t.Setenv("EC_AUSPOST_API_SECRET", "")
	_, err := carrier.ProductionAusPostConfig()
	if err == nil {
		t.Fatal("expected error for missing API secret")
	}
}

func TestProductionAusPostConfigSuccess(t *testing.T) {
	t.Setenv("EC_AUSPOST_API_KEY", "test-key")
	t.Setenv("EC_AUSPOST_API_SECRET", "test-secret")
	cfg, err := carrier.ProductionAusPostConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIKey != "test-key" {
		t.Fatalf("APIKey = %q, want test-key", cfg.APIKey)
	}
	if cfg.APISecret != "test-secret" {
		t.Fatalf("APISecret = %q, want test-secret", cfg.APISecret)
	}
}

func TestProductionDHLConfigMissingKey(t *testing.T) {
	t.Setenv("EC_DHL_API_KEY", "")
	t.Setenv("EC_DHL_API_SECRET", "secret")
	_, err := carrier.ProductionDHLConfig()
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if !errors.Is(err, carrier.ErrCarrierClientUnconfigured) {
		t.Fatalf("error = %v, want ErrCarrierClientUnconfigured", err)
	}
}

func TestProductionDHLConfigSuccess(t *testing.T) {
	t.Setenv("EC_DHL_API_KEY", "dhl-key")
	t.Setenv("EC_DHL_API_SECRET", "dhl-secret")
	cfg, err := carrier.ProductionDHLConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientID != "dhl-key" {
		t.Fatalf("ClientID = %q, want dhl-key", cfg.ClientID)
	}
	if cfg.ClientSecret != "dhl-secret" {
		t.Fatalf("ClientSecret = %q, want dhl-secret", cfg.ClientSecret)
	}
}

func TestAusPostSignedRequestVCR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig := r.Header.Get("X-AusPost-Signature")
		if sig == "" {
			t.Fatal("missing X-AusPost-Signature header")
		}
		key := r.Header.Get("X-AusPost-Key")
		if key != "prod-key" {
			t.Fatalf("X-AusPost-Key = %q, want prod-key", key)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"cost_aud_cents": 1500,
			"eta_days":       3,
		})
	}))
	defer srv.Close()

	client, err := carrier.NewAusPostClient(carrier.AusPostConfig{
		BaseURL:   srv.URL,
		APIKey:    "prod-key",
		APISecret: "prod-secret",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	quote, err := client.Quote(ctx, carrier.QuoteRequest{
		TenantID: "t1", DestPost: "3000", WeightGrams: 500,
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if quote.CostAUDCents != 1500 {
		t.Fatalf("cost = %d, want 1500", quote.CostAUDCents)
	}
}

func TestDHLOAuthTokenExchangeVCR(t *testing.T) {
	oauthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("oauth: want POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			t.Fatalf("grant_type = %q", r.Form.Get("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-bearer-token",
			"expires_in":   3600,
		})
	}))
	defer oauthSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-bearer-token" {
			t.Fatalf("Authorization = %q, want Bearer test-bearer-token", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"cost_aud_cents": 4200,
			"eta_days":       5,
		})
	}))
	defer apiSrv.Close()

	client, err := carrier.NewDHLClient(carrier.DHLConfig{
		BaseURL:      apiSrv.URL,
		OAuthURL:     oauthSrv.URL,
		ClientID:     "dhl-prod-id",
		ClientSecret: "dhl-prod-secret",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	quote, err := client.Quote(ctx, carrier.QuoteRequest{
		TenantID: "t1", DestPost: "2000", WeightGrams: 1000,
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if quote.CostAUDCents != 4200 {
		t.Fatalf("cost = %d, want 4200", quote.CostAUDCents)
	}
}

func TestAusPostHMACVerification(t *testing.T) {
	secret := "hmac-test-secret"
	method := "POST"
	path := "/v3/shipping/quotes"
	body := []byte(`{"tenant_id":"t1"}`)

	ok := carrier.VerifyAusPostHMAC(secret, method, path, body, "wrong-sig")
	if ok {
		t.Fatal("expected HMAC verification to fail with wrong signature")
	}
}

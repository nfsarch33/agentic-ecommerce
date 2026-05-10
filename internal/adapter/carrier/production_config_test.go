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

type stubTokenHelper string

func (s stubTokenHelper) AccessToken(_ context.Context) (string, error) {
	return string(s), nil
}

func stubToken(tok string) carrier.DHLTokenSource { return stubTokenHelper(tok) }

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

func TestResolveAusPostBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		envVal  string
		wantURL string
	}{
		{name: "default (empty) is sandbox", envVal: "", wantURL: "https://digitalapi.auspost.com.au/test/shipping/v1/"},
		{name: "explicit true is sandbox", envVal: "true", wantURL: "https://digitalapi.auspost.com.au/test/shipping/v1/"},
		{name: "1 is sandbox", envVal: "1", wantURL: "https://digitalapi.auspost.com.au/test/shipping/v1/"},
		{name: "false is production", envVal: "false", wantURL: "https://digitalapi.auspost.com.au/shipping/v1/"},
		{name: "0 is production", envVal: "0", wantURL: "https://digitalapi.auspost.com.au/shipping/v1/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("EC_AUSPOST_SANDBOX", tc.envVal)
			got := carrier.ResolveAusPostBaseURL()
			if got != tc.wantURL {
				t.Fatalf("ResolveAusPostBaseURL() = %q, want %q", got, tc.wantURL)
			}
		})
	}
}

func TestResolveDHLBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		envVal  string
		wantURL string
	}{
		{name: "default (empty) is sandbox", envVal: "", wantURL: "https://express.api.dhl.com/mydhlapi/test/"},
		{name: "explicit true is sandbox", envVal: "true", wantURL: "https://express.api.dhl.com/mydhlapi/test/"},
		{name: "1 is sandbox", envVal: "1", wantURL: "https://express.api.dhl.com/mydhlapi/test/"},
		{name: "false is production", envVal: "false", wantURL: "https://express.api.dhl.com/mydhlapi/"},
		{name: "0 is production", envVal: "0", wantURL: "https://express.api.dhl.com/mydhlapi/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("EC_DHL_SANDBOX", tc.envVal)
			got := carrier.ResolveDHLBaseURL()
			if got != tc.wantURL {
				t.Fatalf("ResolveDHLBaseURL() = %q, want %q", got, tc.wantURL)
			}
		})
	}
}

func TestProductionAusPostConfig_UsesSandboxByDefault(t *testing.T) {
	t.Setenv("EC_AUSPOST_API_KEY", "key")
	t.Setenv("EC_AUSPOST_API_SECRET", "secret")
	t.Setenv("EC_AUSPOST_SANDBOX", "")
	cfg, err := carrier.ProductionAusPostConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://digitalapi.auspost.com.au/test/shipping/v1/"
	if cfg.BaseURL != want {
		t.Fatalf("BaseURL = %q, want sandbox %q", cfg.BaseURL, want)
	}
}

func TestProductionAusPostConfig_ProductionWhenSandboxFalse(t *testing.T) {
	t.Setenv("EC_AUSPOST_API_KEY", "key")
	t.Setenv("EC_AUSPOST_API_SECRET", "secret")
	t.Setenv("EC_AUSPOST_SANDBOX", "false")
	cfg, err := carrier.ProductionAusPostConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://digitalapi.auspost.com.au/shipping/v1/"
	if cfg.BaseURL != want {
		t.Fatalf("BaseURL = %q, want production %q", cfg.BaseURL, want)
	}
}

func TestProductionDHLConfig_UsesSandboxByDefault(t *testing.T) {
	t.Setenv("EC_DHL_API_KEY", "key")
	t.Setenv("EC_DHL_API_SECRET", "secret")
	t.Setenv("EC_DHL_SANDBOX", "")
	cfg, err := carrier.ProductionDHLConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://express.api.dhl.com/mydhlapi/test/"
	if cfg.BaseURL != want {
		t.Fatalf("BaseURL = %q, want sandbox %q", cfg.BaseURL, want)
	}
}

func TestProductionDHLConfig_ProductionWhenSandboxFalse(t *testing.T) {
	t.Setenv("EC_DHL_API_KEY", "key")
	t.Setenv("EC_DHL_API_SECRET", "secret")
	t.Setenv("EC_DHL_SANDBOX", "false")
	cfg, err := carrier.ProductionDHLConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://express.api.dhl.com/mydhlapi/"
	if cfg.BaseURL != want {
		t.Fatalf("BaseURL = %q, want production %q", cfg.BaseURL, want)
	}
}

func TestAusPostSandboxLabelVCR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tracking_number": "AP-SB-001",
			"label_pdf_url":   "https://ap-sandbox/labels/sb-001.pdf",
			"cost_aud_cents":  895,
			"eta_days":        5,
		})
	}))
	defer srv.Close()

	client, err := carrier.NewAusPostClient(carrier.AusPostConfig{
		BaseURL:   srv.URL,
		APIKey:    "sandbox-key",
		APISecret: "sandbox-secret",
		Now:       func() time.Time { return time.Unix(1000, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	label, err := client.CreateLabel(context.Background(), carrier.LabelRequest{
		TenantID: "t1", OrderID: "ord-sb", DestPost: "3000", DestCountry: "AU", WeightGrams: 250,
	})
	if err != nil {
		t.Fatalf("create label: %v", err)
	}
	if label.TrackingNumber != "AP-SB-001" {
		t.Fatalf("tracking = %q, want AP-SB-001", label.TrackingNumber)
	}
	if label.CostAUDCents != 895 {
		t.Fatalf("cost = %d, want 895", label.CostAUDCents)
	}
}

func TestDHLSandboxQuoteVCR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer sandbox-token" {
			t.Fatalf("Authorization = %q, want Bearer sandbox-token", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"cost_aud_cents": 3150,
			"eta_days":       2,
		})
	}))
	defer srv.Close()

	client, err := carrier.NewDHLClient(carrier.DHLConfig{
		BaseURL:     srv.URL,
		TokenSource: stubToken("sandbox-token"),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	q, err := client.Quote(context.Background(), carrier.QuoteRequest{
		TenantID: "t1", DestPost: "2000", DestCountry: "AU", WeightGrams: 1200,
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if q.CostAUDCents != 3150 {
		t.Fatalf("cost = %d, want 3150", q.CostAUDCents)
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

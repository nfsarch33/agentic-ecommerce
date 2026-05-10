// File scope: v3.8.1 carry-forward closure -- error-branch coverage
// for the v3.8.0 carrier package.
//
// The v3.8.0 sprint retro flagged a -0.8% coverage delta vs v3.7.1
// driven primarily by un-exercised parse error branches in
// parseAusPostQuote / parseAusPostLabel / parseDHLQuote /
// parseDHLLabel / staticTokenSource. This file closes those
// branches end-to-end so the v3.8.1 coverage recovers to 84%+.
package carrier

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAusPost_ParseQuote_DecodeError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	t.Cleanup(srv.Close)
	client, _ := NewAusPostClient(AusPostConfig{BaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()})
	_, err := client.Quote(context.Background(), QuoteRequest{TenantID: "t1", DestPost: "3000", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLabelGenerationFailed))
}

func TestAusPost_ParseQuote_EmptyBodyTriggersInvalid(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cost_aud_cents":0,"eta_days":0}`))
	}))
	t.Cleanup(srv.Close)
	client, _ := NewAusPostClient(AusPostConfig{BaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()})
	_, err := client.Quote(context.Background(), QuoteRequest{TenantID: "t1", DestPost: "3000", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLabelGenerationFailed))
}

func TestAusPost_ParseLabel_DecodeErrorAndMissingFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{name: "decode-error", body: `not json`},
		{name: "missing-tracking", body: `{"label_pdf_url":"https://x"}`},
		{name: "missing-pdf-url", body: `{"tracking_number":"AP-1"}`},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(c.body))
			}))
			t.Cleanup(srv.Close)
			client, _ := NewAusPostClient(AusPostConfig{BaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()})
			_, err := client.CreateLabel(context.Background(), LabelRequest{TenantID: "t1", OrderID: "ord-1", DestPost: "3000", DestCountry: "AU", WeightGrams: 500})
			require.Error(t, err)
		})
	}
}

func TestAusPost_QuoteAndLabel_4xxMapToLabelGenerationFailed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	client, _ := NewAusPostClient(AusPostConfig{BaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()})
	_, err := client.Quote(context.Background(), QuoteRequest{TenantID: "t1", DestPost: "3000", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLabelGenerationFailed))

	_, err = client.CreateLabel(context.Background(), LabelRequest{TenantID: "t1", OrderID: "ord-1", DestPost: "3000", DestCountry: "AU", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLabelGenerationFailed))
}

func TestAusPost_LabelTransportError_MapsToUnavailable(t *testing.T) {
	t.Parallel()
	// Create + immediately close a server so any subsequent request
	// hits a closed socket -> transport error -> ErrCarrierUnavailable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.Close()
	client, _ := NewAusPostClient(AusPostConfig{BaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()})
	_, err := client.CreateLabel(context.Background(), LabelRequest{TenantID: "t1", OrderID: "ord-1", DestPost: "3000", DestCountry: "AU", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCarrierUnavailable))
}

func TestAusPost_LabelRejectsInvalidShippingAddress(t *testing.T) {
	t.Parallel()
	client, _ := NewAusPostClient(AusPostConfig{BaseURL: "https://example.test", APIKey: "k", APISecret: "s"})
	_, err := client.CreateLabel(context.Background(), LabelRequest{TenantID: "t1", OrderID: "", DestPost: "3000", DestCountry: "AU", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidShippingAddress))
}

func TestDHL_ParseQuote_DecodeError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`bad-json`))
	}))
	t.Cleanup(srv.Close)
	client, _ := NewDHLClient(DHLConfig{BaseURL: srv.URL, TokenSource: stubTokenSource{token: "t"}, HTTPClient: srv.Client()})
	_, err := client.Quote(context.Background(), QuoteRequest{TenantID: "t1", DestPost: "3000", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLabelGenerationFailed))
}

func TestDHL_ParseQuote_EmptyValuesInvalid(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cost_aud_cents":0,"eta_days":0}`))
	}))
	t.Cleanup(srv.Close)
	client, _ := NewDHLClient(DHLConfig{BaseURL: srv.URL, TokenSource: stubTokenSource{token: "t"}, HTTPClient: srv.Client()})
	_, err := client.Quote(context.Background(), QuoteRequest{TenantID: "t1", DestPost: "3000", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLabelGenerationFailed))
}

func TestDHL_ParseLabel_MissingFields(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"label_pdf_url":""}`))
	}))
	t.Cleanup(srv.Close)
	client, _ := NewDHLClient(DHLConfig{BaseURL: srv.URL, TokenSource: stubTokenSource{token: "t"}, HTTPClient: srv.Client()})
	_, err := client.CreateLabel(context.Background(), LabelRequest{TenantID: "t1", OrderID: "ord-1", DestPost: "3000", DestCountry: "AU", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLabelGenerationFailed))
}

func TestDHL_Label4xxMapsToLabelFailed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	client, _ := NewDHLClient(DHLConfig{BaseURL: srv.URL, TokenSource: stubTokenSource{token: "t"}, HTTPClient: srv.Client()})
	_, err := client.CreateLabel(context.Background(), LabelRequest{TenantID: "t1", OrderID: "ord-1", DestPost: "3000", DestCountry: "AU", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLabelGenerationFailed))
}

func TestDHL_Label5xxMapsToUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	client, _ := NewDHLClient(DHLConfig{BaseURL: srv.URL, TokenSource: stubTokenSource{token: "t"}, HTTPClient: srv.Client()})
	_, err := client.CreateLabel(context.Background(), LabelRequest{TenantID: "t1", OrderID: "ord-1", DestPost: "3000", DestCountry: "AU", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCarrierUnavailable))
}

func TestDHL_LabelRejectsInvalidShippingAddress(t *testing.T) {
	t.Parallel()
	client, _ := NewDHLClient(DHLConfig{BaseURL: "https://example.test", TokenSource: stubTokenSource{token: "t"}})
	_, err := client.CreateLabel(context.Background(), LabelRequest{TenantID: "t1", OrderID: "", DestPost: "3000", DestCountry: "AU", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidShippingAddress))
}

func TestDHL_StaticTokenSource_OAuthError(t *testing.T) {
	t.Parallel()
	// OAuth server returns 401 -> ErrCarrierUnavailable surface via
	// the static token source.
	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(oauth.Close)
	client, err := NewDHLClient(DHLConfig{
		BaseURL:      "https://api.test",
		OAuthURL:     oauth.URL,
		ClientID:     "id",
		ClientSecret: "sec",
		HTTPClient:   oauth.Client(),
	})
	require.NoError(t, err)
	_, err = client.tokens.AccessToken(context.Background())
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCarrierUnavailable))
}

func TestDHL_StaticTokenSource_OAuthDecodeError(t *testing.T) {
	t.Parallel()
	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	t.Cleanup(oauth.Close)
	client, err := NewDHLClient(DHLConfig{
		BaseURL:      "https://api.test",
		OAuthURL:     oauth.URL,
		ClientID:     "id",
		ClientSecret: "sec",
		HTTPClient:   oauth.Client(),
	})
	require.NoError(t, err)
	_, err = client.tokens.AccessToken(context.Background())
	require.Error(t, err)
}

func TestDHL_StaticTokenSource_OAuthEmptyToken(t *testing.T) {
	t.Parallel()
	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"","expires_in":0}`))
	}))
	t.Cleanup(oauth.Close)
	client, err := NewDHLClient(DHLConfig{
		BaseURL:      "https://api.test",
		OAuthURL:     oauth.URL,
		ClientID:     "id",
		ClientSecret: "sec",
		HTTPClient:   oauth.Client(),
	})
	require.NoError(t, err)
	_, err = client.tokens.AccessToken(context.Background())
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCarrierUnavailable))
}

func TestDHL_DoTransportError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.Close()
	client, _ := NewDHLClient(DHLConfig{BaseURL: srv.URL, TokenSource: stubTokenSource{token: "t"}, HTTPClient: srv.Client()})
	_, err := client.Quote(context.Background(), QuoteRequest{TenantID: "t1", DestPost: "3000", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCarrierUnavailable))
}

func TestVerifyAusPostHMAC_RejectsTamperedSig(t *testing.T) {
	t.Parallel()
	body := []byte(`{"a":1}`)
	good := signAusPost("s", "POST", "/v3/shipping/labels", body)
	require.True(t, VerifyAusPostHMAC("s", "POST", "/v3/shipping/labels", body, good))
	require.False(t, VerifyAusPostHMAC("s", "POST", "/v3/shipping/labels", body, ""))
	require.False(t, VerifyAusPostHMAC("s", "POST", "/v3/shipping/labels", body, strings.Repeat("0", len(good))))
}

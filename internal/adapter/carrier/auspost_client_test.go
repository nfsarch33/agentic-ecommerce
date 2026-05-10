package carrier

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAusPostClient_QuoteParsesPriceAndETA(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/shipping/quotes", r.URL.Path)
		require.NotEmpty(t, r.Header.Get("X-AusPost-Signature"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cost_aud_cents":1299,"eta_days":4}`))
	}))
	t.Cleanup(srv.Close)

	client, err := NewAusPostClient(AusPostConfig{
		BaseURL:    srv.URL,
		APIKey:     "k",
		APISecret:  "s",
		HTTPClient: srv.Client(),
	})
	require.NoError(t, err)

	quote, err := client.Quote(context.Background(), QuoteRequest{
		TenantID: "t1", DestPost: "3000", DestCountry: "AU", WeightGrams: 500,
	})
	require.NoError(t, err)
	require.Equal(t, CarrierAusPost, quote.Carrier)
	require.Equal(t, 1299, quote.CostAUDCents)
	require.Equal(t, 4, quote.ETADays)
}

func TestAusPostClient_CreateLabelReturnsTrackingAndPDF(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/shipping/labels", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tracking_number":"AP123","label_pdf_url":"https://ap/labels/123.pdf","cost_aud_cents":1299,"eta_days":4}`))
	}))
	t.Cleanup(srv.Close)

	client, err := NewAusPostClient(AusPostConfig{BaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client(), Now: func() time.Time { return time.Unix(1000, 0).UTC() }})
	require.NoError(t, err)

	label, err := client.CreateLabel(context.Background(), LabelRequest{
		TenantID: "t1", OrderID: "ord-1", DestPost: "3000", DestCountry: "AU", WeightGrams: 500,
	})
	require.NoError(t, err)
	require.Equal(t, "AP123", label.TrackingNumber)
	require.Equal(t, "https://ap/labels/123.pdf", label.LabelPDFURL)
	require.Equal(t, 1299, label.CostAUDCents)
	require.Equal(t, 4, label.ETADays)
	require.Equal(t, CarrierAusPost, label.Carrier)
}

func TestAusPostClient_QuoteServerError_MapsToUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	client, _ := NewAusPostClient(AusPostConfig{BaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()})
	_, err := client.Quote(context.Background(), QuoteRequest{TenantID: "t1", DestPost: "3000", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCarrierUnavailable))
}

func TestAusPostClient_RejectsInvalidShippingAddress(t *testing.T) {
	t.Parallel()
	client, _ := NewAusPostClient(AusPostConfig{BaseURL: "https://example.test", APIKey: "k", APISecret: "s"})
	_, err := client.Quote(context.Background(), QuoteRequest{TenantID: "t1", DestPost: "", WeightGrams: 500})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidShippingAddress))
}

func TestAusPostClient_RequiresBaseURL(t *testing.T) {
	t.Parallel()
	_, err := NewAusPostClient(AusPostConfig{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCarrierClientUnconfigured))
}

func TestVerifyAusPostHMAC_AcceptsCorrectSignature(t *testing.T) {
	t.Parallel()
	body := []byte(`{"k":"v"}`)
	sig := signAusPost("secret", "POST", "/v3/shipping/labels", body)
	require.True(t, VerifyAusPostHMAC("secret", "POST", "/v3/shipping/labels", body, sig))
	require.False(t, VerifyAusPostHMAC("secret", "POST", "/v3/shipping/labels", body, sig+"x"))
	require.False(t, VerifyAusPostHMAC("wrong", "POST", "/v3/shipping/labels", body, sig))
	require.True(t, strings.HasPrefix(sig, ""))
}

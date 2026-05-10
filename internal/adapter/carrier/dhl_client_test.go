package carrier

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type stubTokenSource struct{ token string }

func (s stubTokenSource) AccessToken(_ context.Context) (string, error) { return s.token, nil }

func TestDHLClient_QuoteParsesPriceAndETA(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
		require.Equal(t, "/express/quotes", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cost_aud_cents":2599,"eta_days":3}`))
	}))
	t.Cleanup(srv.Close)

	client, err := NewDHLClient(DHLConfig{
		BaseURL:     srv.URL,
		TokenSource: stubTokenSource{token: "abc"},
		HTTPClient:  srv.Client(),
	})
	require.NoError(t, err)

	q, err := client.Quote(context.Background(), QuoteRequest{TenantID: "t1", DestPost: "3000", DestCountry: "AU", WeightGrams: 500})
	require.NoError(t, err)
	require.Equal(t, CarrierDHL, q.Carrier)
	require.Equal(t, 2599, q.CostAUDCents)
	require.Equal(t, 3, q.ETADays)
}

func TestDHLClient_CreateLabelReturnsTrackingAndPDF(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/express/labels", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tracking_number":"DHL-9","label_pdf_url":"https://dhl/labels/9.pdf","cost_aud_cents":2599,"eta_days":3}`))
	}))
	t.Cleanup(srv.Close)

	client, err := NewDHLClient(DHLConfig{
		BaseURL:     srv.URL,
		TokenSource: stubTokenSource{token: "abc"},
		HTTPClient:  srv.Client(),
		Now:         func() time.Time { return time.Unix(2000, 0).UTC() },
	})
	require.NoError(t, err)

	label, err := client.CreateLabel(context.Background(), LabelRequest{TenantID: "t1", OrderID: "ord-2", DestPost: "3000", DestCountry: "AU", WeightGrams: 500})
	require.NoError(t, err)
	require.Equal(t, "DHL-9", label.TrackingNumber)
	require.Equal(t, "https://dhl/labels/9.pdf", label.LabelPDFURL)
	require.Equal(t, 3, label.ETADays)
}

func TestDHLClient_QuoteServerError_MapsToUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	client, _ := NewDHLClient(DHLConfig{BaseURL: srv.URL, TokenSource: stubTokenSource{token: "abc"}, HTTPClient: srv.Client()})
	_, err := client.Quote(context.Background(), QuoteRequest{TenantID: "t1", DestPost: "3000", WeightGrams: 500})
	require.True(t, errors.Is(err, ErrCarrierUnavailable))
}

func TestDHLClient_RequiresBaseURL(t *testing.T) {
	t.Parallel()
	_, err := NewDHLClient(DHLConfig{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCarrierClientUnconfigured))
}

func TestDHLClient_RequiresTokenSourceOrOAuth(t *testing.T) {
	t.Parallel()
	_, err := NewDHLClient(DHLConfig{BaseURL: "https://example.test"})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCarrierClientUnconfigured))
}

func TestDHLClient_StaticTokenSourceCachesAndReuses(t *testing.T) {
	t.Parallel()
	hits := 0
	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"t1","expires_in":3600}`))
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
	require.NotNil(t, client.tokens)
	for i := 0; i < 3; i++ {
		_, err := client.tokens.AccessToken(context.Background())
		require.NoError(t, err)
	}
	require.Equal(t, 1, hits, "token source must cache between calls")
}

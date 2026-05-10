package carrier

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBothCarriers_QuoteValidation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"cost_aud_cents":100,"eta_days":1}`))
	}))
	t.Cleanup(srv.Close)

	auspost, err := NewAusPostClient(AusPostConfig{BaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()})
	require.NoError(t, err)
	dhl, err := NewDHLClient(DHLConfig{BaseURL: srv.URL, TokenSource: stubTokenSource{token: "t"}, HTTPClient: srv.Client()})
	require.NoError(t, err)

	type quoter interface {
		Quote(ctx context.Context, req QuoteRequest) (Quote, error)
	}
	carriers := []struct {
		name string
		c    quoter
	}{
		{"auspost", auspost},
		{"dhl", dhl},
	}

	validationCases := []struct {
		caseName string
		req      QuoteRequest
		wantErr  error
	}{
		{"empty_tenant", QuoteRequest{TenantID: "", DestPost: "3000", WeightGrams: 500}, ErrInvalidShippingAddress},
		{"empty_dest", QuoteRequest{TenantID: "t1", DestPost: "", WeightGrams: 500}, ErrInvalidShippingAddress},
		{"zero_weight", QuoteRequest{TenantID: "t1", DestPost: "3000", WeightGrams: 0}, ErrInvalidShippingAddress},
		{"negative_weight", QuoteRequest{TenantID: "t1", DestPost: "3000", WeightGrams: -1}, ErrInvalidShippingAddress},
	}

	for _, c := range carriers {
		for _, tc := range validationCases {
			t.Run(c.name+"/"+tc.caseName, func(t *testing.T) {
				t.Parallel()
				_, err := c.c.Quote(context.Background(), tc.req)
				require.Error(t, err)
				require.True(t, errors.Is(err, tc.wantErr))
			})
		}
	}
}

func TestBothCarriers_ServerError5xx(t *testing.T) {
	t.Parallel()
	statusCodes := []struct {
		name string
		code int
	}{
		{"500", 500},
		{"502", 502},
		{"503", 503},
	}

	for _, sc := range statusCodes {
		t.Run("auspost/"+sc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(sc.code)
			}))
			t.Cleanup(srv.Close)
			c, _ := NewAusPostClient(AusPostConfig{BaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()})
			_, err := c.Quote(context.Background(), QuoteRequest{TenantID: "t1", DestPost: "3000", WeightGrams: 500})
			require.True(t, errors.Is(err, ErrCarrierUnavailable))
		})
		t.Run("dhl/"+sc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(sc.code)
			}))
			t.Cleanup(srv.Close)
			c, _ := NewDHLClient(DHLConfig{BaseURL: srv.URL, TokenSource: stubTokenSource{token: "t"}, HTTPClient: srv.Client()})
			_, err := c.Quote(context.Background(), QuoteRequest{TenantID: "t1", DestPost: "3000", WeightGrams: 500})
			require.True(t, errors.Is(err, ErrCarrierUnavailable))
		})
	}
}

func TestBothCarriers_ConstructorValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		buildFn func() error
		wantErr error
	}{
		{"auspost_no_baseurl", func() error { _, err := NewAusPostClient(AusPostConfig{}); return err }, ErrCarrierClientUnconfigured},
		{"dhl_no_baseurl", func() error { _, err := NewDHLClient(DHLConfig{}); return err }, ErrCarrierClientUnconfigured},
		{"dhl_no_token_no_oauth", func() error { _, err := NewDHLClient(DHLConfig{BaseURL: "https://x.test"}); return err }, ErrCarrierClientUnconfigured},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.buildFn()
			require.Error(t, err)
			require.True(t, errors.Is(err, tc.wantErr))
		})
	}
}

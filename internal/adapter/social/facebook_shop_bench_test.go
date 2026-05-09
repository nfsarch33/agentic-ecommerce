package social

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkFacebookSign measures HMAC-SHA256 appsecret_proof
// throughput. Lets the v3.4.x QA sprint pin the regression
// baseline.
func BenchmarkFacebookSign(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = ComputeFacebookAppSecretProof([]byte(testFacebookSecret), "EAAB-test-page-token-fixture")
	}
}

func BenchmarkFacebookCreateProduct(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"fb-bench"}`))
	}))
	defer srv.Close()
	t := &testing.T{}
	client := mustNewFakeFBClient(t, srv.URL, srv.Client(), nil)
	payload := FacebookProductPayload{TenantID: "tenant-fb", RetailerID: "r", Name: "p", PriceCents: 100, Currency: "AUD"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.CreateProduct(context.Background(), payload)
	}
}

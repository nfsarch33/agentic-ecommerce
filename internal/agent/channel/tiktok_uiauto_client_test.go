package channel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/social"
)

const testBridgeSecret = "omniparser-bridge-test-secret-bytes-fixture" // gitleaks:allow

// TestTikTokUIAutoClient_PostsViaOmniparserBridge is the EC-3-5
// RED acceptance test. Sends a signed POST to the bridge and
// asserts the returned post_id round-trips through the facade.
func TestTikTokUIAutoClient_PostsViaOmniparserBridge(t *testing.T) {
	t.Parallel()

	var capturedBody []byte
	var capturedSig string
	var capturedTenant string
	var capturedTimestamp string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = body
		capturedSig = r.Header.Get("X-Bridge-Sign")
		capturedTenant = r.Header.Get("X-Bridge-Tenant")
		capturedTimestamp = r.Header.Get("X-Bridge-Timestamp")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"post_id":     "uiauto-post-1",
			"status":      "scheduled",
			"occurred_at": "2026-05-09T12:00:00Z",
		})
	}))
	t.Cleanup(srv.Close)

	client, err := NewTikTokUIAutoClient(nil, TikTokUIAutoClientConfig{
		HTTPClient:   srv.Client(),
		BridgeURL:    srv.URL,
		BridgeSecret: []byte(testBridgeSecret),
		TenantID:     "tenant-1",
		Now:          func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewTikTokUIAutoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })

	res, err := client.Post(context.Background(), TikTokOrganicPost{
		TenantID:   "tenant-1",
		ProductID:  "p-1",
		VideoURL:   "https://cdn.example.com/v.mp4",
		Caption:    "Check this out",
		Hashtags:   []string{"#fyp", "#shopping"},
		SessionRef: "sess-alias-1",
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if res.PostID != "uiauto-post-1" || res.Status != "scheduled" {
		t.Fatalf("res = %+v", res)
	}
	if capturedSig == "" {
		t.Fatalf("missing X-Bridge-Sign")
	}
	if capturedTenant != "tenant-1" {
		t.Fatalf("tenant = %q", capturedTenant)
	}
	if capturedTimestamp == "" {
		t.Fatalf("missing X-Bridge-Timestamp")
	}
	if !strings.Contains(string(capturedBody), `"product_id":"p-1"`) {
		t.Fatalf("body missing product_id: %s", capturedBody)
	}
	// Recompute signature with the same params; assert it matches.
	want, sigErr := social.ComputeTikTokSignature(social.TikTokSignRequest{
		Secret:    []byte(testBridgeSecret),
		Timestamp: time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC).Unix(),
		Path:      "/uiauto/tiktok/post",
		Body:      capturedBody,
	})
	if sigErr != nil {
		t.Fatalf("ComputeTikTokSignature: %v", sigErr)
	}
	if capturedSig != want {
		t.Fatalf("sig mismatch got=%s want=%s", capturedSig, want)
	}
}

func TestTikTokUIAutoClient_BridgeRejectionMaps(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "not allowed")
	}))
	t.Cleanup(srv.Close)
	client, err := NewTikTokUIAutoClient(nil, TikTokUIAutoClientConfig{
		HTTPClient: srv.Client(), BridgeURL: srv.URL, BridgeSecret: []byte(testBridgeSecret), TenantID: "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewTikTokUIAutoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	_, err = client.Post(context.Background(), TikTokOrganicPost{ProductID: "p"})
	if !errors.Is(err, ErrUIAutoBridgeRejected) {
		t.Fatalf("err = %v", err)
	}
}

func TestTikTokUIAutoClient_DecodeError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "not-json")
	}))
	t.Cleanup(srv.Close)
	client, err := NewTikTokUIAutoClient(nil, TikTokUIAutoClientConfig{
		HTTPClient: srv.Client(), BridgeURL: srv.URL, BridgeSecret: []byte(testBridgeSecret), TenantID: "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewTikTokUIAutoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	_, err = client.Post(context.Background(), TikTokOrganicPost{ProductID: "p"})
	if err == nil {
		t.Fatalf("expected decode error")
	}
}

func TestTikTokUIAutoClient_RejectsAfterClose(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)
	client, err := NewTikTokUIAutoClient(nil, TikTokUIAutoClientConfig{
		HTTPClient: srv.Client(), BridgeURL: srv.URL, BridgeSecret: []byte(testBridgeSecret), TenantID: "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewTikTokUIAutoClient: %v", err)
	}
	_ = client.Close(context.Background())
	_, err = client.Post(context.Background(), TikTokOrganicPost{ProductID: "p"})
	if !errors.Is(err, ErrUIAutoClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewTikTokUIAutoClient_Validation(t *testing.T) {
	t.Parallel()
	cases := map[string]TikTokUIAutoClientConfig{
		"missing bridge url": {BridgeSecret: []byte(testBridgeSecret), TenantID: "t"},
		"short secret":       {BridgeURL: "http://x", BridgeSecret: []byte("short"), TenantID: "t"},
		"missing tenant":     {BridgeURL: "http://x", BridgeSecret: []byte(testBridgeSecret)},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewTikTokUIAutoClient(nil, cfg)
			if !errors.Is(err, ErrUIAutoUnconfigured) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestTikTokUIAutoClient_HookFires(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"post_id":"x","status":"ok","occurred_at":""}`))
	}))
	t.Cleanup(srv.Close)
	metrics := &recordingMetrics{}
	client, err := NewTikTokUIAutoClient(nil, TikTokUIAutoClientConfig{
		HTTPClient:   srv.Client(),
		BridgeURL:    srv.URL,
		BridgeSecret: []byte(testBridgeSecret),
		TenantID:     "tenant-1",
		Metrics:      metrics,
	})
	if err != nil {
		t.Fatalf("NewTikTokUIAutoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	if _, err := client.Post(context.Background(), TikTokOrganicPost{ProductID: "p"}); err != nil {
		t.Fatalf("Post: %v", err)
	}
	got := metrics.outcomes()
	if len(got) != 1 || got[0] != "uiauto.ok" {
		t.Fatalf("metrics = %v", got)
	}
}

func TestRequireString_FallsBackWhenEmpty(t *testing.T) {
	t.Parallel()
	if requireString("a", "b") != "a" {
		t.Fatal("explicit wins")
	}
	if requireString("", "b") != "b" {
		t.Fatal("fallback")
	}
}

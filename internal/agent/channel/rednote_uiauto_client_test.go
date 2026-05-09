package channel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/social"
)

const testRedNoteSecret = "rednote-bridge-test-secret-bytes-fixture" // gitleaks:allow

// seedRedNoteCookieFixture is a placeholder factory used by the
// EC-4-1 RED test. The real session cookie pickup happens
// server-side in the bridge; this fixture simulates the operator-
// bootstrapped session reference the agent forwards.
func seedRedNoteCookieFixture() string { return "rn-session-alias-001" }

type recordingRedNoteMetrics struct {
	calls atomic.Int64
	last  []string
}

func (r *recordingRedNoteMetrics) RecordRedNoteBridgeCall(_, status string) {
	r.calls.Add(1)
	r.last = append(r.last, status)
}

// TestRedNoteUIAutoClient_PostsViaOmniparserBridge is the EC-4-1
// RED acceptance test. Sends a signed POST to the bridge and
// asserts the returned note_id round-trips through the facade.
func TestRedNoteUIAutoClient_PostsViaOmniparserBridge(t *testing.T) {
	t.Parallel()

	var capturedBody []byte
	var capturedSig string
	var capturedTenant string
	var capturedTimestamp string
	var capturedPlatform string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = body
		capturedSig = r.Header.Get("X-Bridge-Sign")
		capturedTenant = r.Header.Get("X-Bridge-Tenant")
		capturedTimestamp = r.Header.Get("X-Bridge-Timestamp")
		capturedPlatform = r.Header.Get("X-Bridge-Platform")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"note_id":     "xhs-note-001",
			"status":      "scheduled",
			"occurred_at": "2026-05-09T12:00:00Z",
		})
	}))
	t.Cleanup(srv.Close)

	client, err := NewRedNoteUIAutoClient(nil, RedNoteUIAutoClientConfig{
		HTTPClient:   srv.Client(),
		BridgeURL:    srv.URL,
		BridgeSecret: []byte(testRedNoteSecret),
		TenantID:     "tenant-1",
		Now:          func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewRedNoteUIAutoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })

	res, err := client.Post(context.Background(), RedNoteOrganicPost{
		TenantID:   "tenant-1",
		ProductID:  "p-1",
		ImageURLs:  []string{"https://cdn.example.com/img1.jpg", "https://cdn.example.com/img2.jpg"},
		Caption:    "Lifestyle shot of the Wireless Earbuds 🎧",
		Hashtags:   []string{"#xhs", "#earbuds"},
		Topics:     []string{"耳机推荐", "数码好物"},
		SessionRef: seedRedNoteCookieFixture(),
	})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if res.NoteID != "xhs-note-001" || res.Status != "scheduled" {
		t.Fatalf("res = %+v", res)
	}
	if capturedSig == "" {
		t.Fatalf("missing X-Bridge-Sign")
	}
	if capturedTenant != "tenant-1" {
		t.Fatalf("tenant header = %q", capturedTenant)
	}
	if capturedTimestamp == "" {
		t.Fatalf("missing X-Bridge-Timestamp")
	}
	if capturedPlatform != RedNoteBridgePlatform {
		t.Fatalf("platform header = %q, want %q", capturedPlatform, RedNoteBridgePlatform)
	}
	if !strings.Contains(string(capturedBody), `"product_id":"p-1"`) {
		t.Fatalf("body missing product_id: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"session_ref":"rn-session-alias-001"`) {
		t.Fatalf("body missing session_ref: %s", capturedBody)
	}
	// Recompute signature with the same inputs; assert it matches.
	want, sigErr := social.ComputeTikTokSignature(social.TikTokSignRequest{
		Secret:    []byte(testRedNoteSecret),
		Timestamp: time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC).Unix(),
		Path:      RedNoteBridgePostPath,
		Body:      capturedBody,
	})
	if sigErr != nil {
		t.Fatalf("ComputeTikTokSignature: %v", sigErr)
	}
	if capturedSig != want {
		t.Fatalf("sig mismatch got=%s want=%s", capturedSig, want)
	}
}

func TestRedNoteUIAutoClient_BridgeRejectionMaps(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "not allowed")
	}))
	t.Cleanup(srv.Close)
	client, err := NewRedNoteUIAutoClient(nil, RedNoteUIAutoClientConfig{
		HTTPClient: srv.Client(), BridgeURL: srv.URL, BridgeSecret: []byte(testRedNoteSecret), TenantID: "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewRedNoteUIAutoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	_, err = client.Post(context.Background(), RedNoteOrganicPost{ProductID: "p"})
	if !errors.Is(err, ErrRedNoteBridgeRejected) {
		t.Fatalf("err = %v, want ErrRedNoteBridgeRejected", err)
	}
}

func TestRedNoteUIAutoClient_BridgeUnreachableMaps(t *testing.T) {
	t.Parallel()
	// Use a non-routable URL so the HTTP transport fails fast.
	client, err := NewRedNoteUIAutoClient(nil, RedNoteUIAutoClientConfig{
		BridgeURL:    "http://127.0.0.1:1", // closed port
		BridgeSecret: []byte(testRedNoteSecret),
		TenantID:     "tenant-1",
		HTTPClient:   &http.Client{Timeout: 200 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("NewRedNoteUIAutoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	_, err = client.Post(context.Background(), RedNoteOrganicPost{ProductID: "p"})
	if !errors.Is(err, ErrRedNoteBridgeUnreachable) {
		t.Fatalf("err = %v, want ErrRedNoteBridgeUnreachable", err)
	}
}

func TestRedNoteUIAutoClient_DecodeError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "not-json")
	}))
	t.Cleanup(srv.Close)
	client, err := NewRedNoteUIAutoClient(nil, RedNoteUIAutoClientConfig{
		HTTPClient: srv.Client(), BridgeURL: srv.URL, BridgeSecret: []byte(testRedNoteSecret), TenantID: "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewRedNoteUIAutoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	_, err = client.Post(context.Background(), RedNoteOrganicPost{ProductID: "p"})
	if err == nil {
		t.Fatalf("expected decode error")
	}
}

func TestRedNoteUIAutoClient_RejectsAfterClose(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)
	client, err := NewRedNoteUIAutoClient(nil, RedNoteUIAutoClientConfig{
		HTTPClient: srv.Client(), BridgeURL: srv.URL, BridgeSecret: []byte(testRedNoteSecret), TenantID: "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewRedNoteUIAutoClient: %v", err)
	}
	_ = client.Close(context.Background())
	_, err = client.Post(context.Background(), RedNoteOrganicPost{ProductID: "p"})
	if !errors.Is(err, ErrRedNoteClosed) {
		t.Fatalf("err = %v, want ErrRedNoteClosed", err)
	}
}

func TestNewRedNoteUIAutoClient_Validation(t *testing.T) {
	t.Parallel()
	cases := map[string]RedNoteUIAutoClientConfig{
		"missing bridge url": {BridgeSecret: []byte(testRedNoteSecret), TenantID: "t"},
		"short secret":       {BridgeURL: "http://x", BridgeSecret: []byte("short"), TenantID: "t"},
		"missing tenant":     {BridgeURL: "http://x", BridgeSecret: []byte(testRedNoteSecret)},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewRedNoteUIAutoClient(nil, cfg)
			if !errors.Is(err, ErrRedNoteUnconfigured) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestRedNoteUIAutoClient_HookFires(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"note_id":"x","status":"ok","occurred_at":""}`))
	}))
	t.Cleanup(srv.Close)
	metrics := &recordingRedNoteMetrics{}
	client, err := NewRedNoteUIAutoClient(nil, RedNoteUIAutoClientConfig{
		HTTPClient:   srv.Client(),
		BridgeURL:    srv.URL,
		BridgeSecret: []byte(testRedNoteSecret),
		TenantID:     "tenant-1",
		Metrics:      metrics,
	})
	if err != nil {
		t.Fatalf("NewRedNoteUIAutoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	if _, err := client.Post(context.Background(), RedNoteOrganicPost{ProductID: "p"}); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if metrics.calls.Load() != 1 || metrics.last[0] != "ok" {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestRedNoteUIAutoClient_DefaultsApplied(t *testing.T) {
	t.Parallel()
	client, err := NewRedNoteUIAutoClient(nil, RedNoteUIAutoClientConfig{
		BridgeURL:    "http://x",
		BridgeSecret: []byte(testRedNoteSecret),
		TenantID:     "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewRedNoteUIAutoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	if client.cfg.HTTPClient == nil {
		t.Fatal("HTTPClient default missing")
	}
	if client.cfg.UserAgent == "" {
		t.Fatal("UserAgent default missing")
	}
	if client.cfg.Now == nil {
		t.Fatal("Now default missing")
	}
}

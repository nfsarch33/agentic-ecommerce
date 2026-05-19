package minimax

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// File scope: targeted coverage for previously-uncovered branches in
// MiniMax bridge client (Embed transport, Complete error paths,
// validateBridgeURL/isLocalHost edge cases).

func TestEmbedReturnsEmptySliceWhenNoTexts(t *testing.T) {
	t.Parallel()

	client, err := NewClient(Config{BridgeURL: "https://bridge.example/v1"}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	got, err := client.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Embed result = %d, want 0", len(got))
	}
}

func TestEmbedReturnsErrorWhenServerStatusBad(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Config{BridgeURL: server.URL + "/v1", AllowTestLocalhost: true}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Embed(context.Background(), []string{"hello"}); err == nil {
		t.Fatal("expected non-2xx error from Embed")
	}
}

func TestEmbedReturnsErrorWhenIndexOutOfRange(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"index":42,"embedding":[1.0,2.0,3.0]}]}`)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Config{BridgeURL: server.URL, AllowTestLocalhost: true}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.Embed(context.Background(), []string{"hello"})
	if err == nil || !strings.Contains(err.Error(), "invalid index") {
		t.Fatalf("err = %v, want invalid index error", err)
	}
}

func TestEmbedReturnsErrorWhenMissingEmbeddingForIndex(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[]}]}`)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Config{BridgeURL: server.URL, AllowTestLocalhost: true}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.Embed(context.Background(), []string{"hello"})
	if err == nil || !strings.Contains(err.Error(), "missing embedding") {
		t.Fatalf("err = %v, want missing embedding error", err)
	}
}

func TestEmbedDecodesValidBridgeResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[0.1,0.2]},{"index":1,"embedding":[0.3,0.4]}]}`)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Config{BridgeURL: server.URL, AllowTestLocalhost: true}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	got, err := client.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 2 || got[0][0] != 0.1 || got[1][1] != 0.4 {
		t.Fatalf("Embed result = %v", got)
	}
}

func TestCompleteRejectsEmptyMessages(t *testing.T) {
	t.Parallel()

	client, err := NewClient(Config{BridgeURL: "https://bridge.example/v1"}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.Complete(context.Background(), port.AICompletionRequest{})
	if err == nil || !strings.Contains(err.Error(), "messages must not be empty") {
		t.Fatalf("err = %v, want messages error", err)
	}
}

func TestCompletePropagatesNon2xxStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Config{BridgeURL: server.URL, AllowTestLocalhost: true}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.Complete(context.Background(), port.AICompletionRequest{Messages: []port.AIMessage{{Role: "user", Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "status 429") {
		t.Fatalf("err = %v, want status 429 surfacing", err)
	}
}

func TestCompleteReturnsErrorWhenChoicesEmpty(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"total_tokens":0}}`)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Config{BridgeURL: server.URL, AllowTestLocalhost: true}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.Complete(context.Background(), port.AICompletionRequest{Messages: []port.AIMessage{{Role: "user", Content: "ping"}}})
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("err = %v, want no choices error", err)
	}
}

func TestCompleteHonoursPerRequestModelOverride(t *testing.T) {
	t.Parallel()

	var seenModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatCompletionRequest
		if err := decodeJSON(r.Body, &req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		seenModel = req.Model
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":3}}`)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Config{BridgeURL: server.URL, Model: "default-model", AllowTestLocalhost: true}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Complete(context.Background(), port.AICompletionRequest{
		Model:    "override-model",
		Messages: []port.AIMessage{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if seenModel != "override-model" {
		t.Fatalf("server saw model = %q, want override-model", seenModel)
	}
}

func TestValidateBridgeURLAcceptsLocalhostInTestMode(t *testing.T) {
	t.Parallel()

	got, err := validateBridgeURL("http://127.0.0.1:8080/v1", true)
	if err != nil {
		t.Fatalf("validateBridgeURL: %v", err)
	}
	if got != "http://127.0.0.1:8080/v1" {
		t.Fatalf("got = %q", got)
	}
}

func TestValidateBridgeURLRejectsLocalhostWithoutTestMode(t *testing.T) {
	t.Parallel()

	if _, err := validateBridgeURL("http://localhost/v1", false); !errors.Is(err, ErrLocalBridgeURL) {
		t.Fatalf("err = %v, want ErrLocalBridgeURL", err)
	}
}

func TestValidateBridgeURLRejectsDirectMiniMaxURLs(t *testing.T) {
	t.Parallel()

	if _, err := validateBridgeURL("https://api.minimaxi.com/v1", true); !errors.Is(err, ErrDirectMiniMaxURL) {
		t.Fatalf("err = %v, want ErrDirectMiniMaxURL", err)
	}
}

func TestValidateBridgeURLRejectsUnsupportedScheme(t *testing.T) {
	t.Parallel()

	if _, err := validateBridgeURL("ftp://bridge.example/v1", true); err == nil {
		t.Fatal("expected unsupported scheme error")
	}
}

func TestIsLocalHostHandlesIPv6Loopback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		host string
		want bool
	}{
		{host: "localhost", want: true},
		{host: "127.0.0.1", want: true},
		{host: "::1", want: true},
		{host: "8.8.8.8", want: false},
		{host: "bridge.example", want: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.host, func(t *testing.T) {
			t.Parallel()
			if got := isLocalHost(tc.host); got != tc.want {
				t.Fatalf("isLocalHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

// decodeJSON is a tiny helper used only by tests to avoid pulling
// json.Decoder into multiple files.
func decodeJSON(r io.Reader, out any) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

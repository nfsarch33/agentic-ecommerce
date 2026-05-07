package minimax_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/minimax"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

func TestNewClientRejectsDirectMiniMaxURL(t *testing.T) {
	t.Parallel()

	_, err := minimax.NewClient(minimax.Config{BridgeURL: "https://api.minimaxi.com/v1"}, nil)
	if err == nil {
		t.Fatal("expected direct MiniMax URL to be rejected")
	}
}

func TestNewClientRejectsLocalhostUnlessTestMode(t *testing.T) {
	t.Parallel()

	_, err := minimax.NewClient(minimax.Config{BridgeURL: "http://127.0.0.1:8088"}, nil)
	if err == nil {
		t.Fatal("expected localhost bridge URL to be rejected")
	}
}

func TestClientCallsFleetBridgeWithoutMiniMaxAPIKey(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "must-not-be-forwarded")

	var gotPath string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["model"] != "minimax-text-01" {
			t.Fatalf("model = %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"generated content"}}],"usage":{"total_tokens":33}}`))
	}))
	defer server.Close()

	client, err := minimax.NewClient(minimax.Config{
		BridgeURL:          server.URL,
		Model:              "minimax-text-01",
		AllowTestLocalhost: true,
	}, server.Client())
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	resp, err := client.Complete(context.Background(), port.AICompletionRequest{
		Messages: []port.AIMessage{{Role: "user", Content: "write"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization header = %q, want empty", gotAuth)
	}
	if resp.Content != "generated content" || resp.TokensUsed != 33 {
		t.Fatalf("response = %+v", resp)
	}
}

func TestClientAcceptsBridgeURLWithV1Path(t *testing.T) {
	t.Parallel()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`))
	}))
	defer server.Close()

	client, err := minimax.NewClient(minimax.Config{
		BridgeURL:          server.URL + "/v1",
		AllowTestLocalhost: true,
	}, server.Client())
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	_, err = client.Complete(context.Background(), port.AICompletionRequest{
		Messages: []port.AIMessage{{Role: "user", Content: "write"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
	}
}

func TestClientReportsBridgeErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bridge down", http.StatusBadGateway)
	}))
	defer server.Close()
	client, err := minimax.NewClient(minimax.Config{BridgeURL: server.URL, AllowTestLocalhost: true}, server.Client())
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	_, err = client.Complete(context.Background(), port.AICompletionRequest{
		Messages: []port.AIMessage{{Role: "user", Content: "write"}},
	})
	if err == nil {
		t.Fatal("expected bridge error")
	}
}

func TestNewClientValidatesBridgeURL(t *testing.T) {
	t.Parallel()

	_, err := minimax.NewClient(minimax.Config{}, nil)
	if !errors.Is(err, minimax.ErrMissingBridgeURL) {
		t.Fatalf("err = %v, want ErrMissingBridgeURL", err)
	}
}

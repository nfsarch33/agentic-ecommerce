package shopify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync"
)

func TestQANewClientUsesBoundedDefaultHTTPTimeout(t *testing.T) {
	t.Parallel()

	client := mustDefaultHTTPClient(t)
	requireBoundedTimeout(t, client.httpClient)
}

func mustDefaultHTTPClient(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient(Config{BaseURL: "https://example.invalid", AccessToken: "token-test"}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func requireBoundedTimeout(t *testing.T, client *http.Client) {
	t.Helper()
	if client == nil {
		t.Fatal("http client is nil")
	}
	if client.Timeout <= 0 || client.Timeout > 30*time.Second {
		t.Fatalf("default timeout = %s, want bounded positive timeout <= 30s", client.Timeout)
	}
}

func TestQACassettesContainNoCredentialMarkers(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "product_set_success.json")
	if err := scanFixtureFile(path, shopifyCredentialMarkers()); err != nil {
		t.Fatal(err)
	}
}

func TestQAUserErrorsFlowThroughSharedEngineToDLQ(t *testing.T) {
	t.Parallel()

	server, calls := userErrorServer(t)
	client := mustTestClient(t, server)
	dlq := marketplacesync.NewInMemoryDLQ()
	engine := mustTestEngine(t, client, dlq)

	result, err := engine.Sync(context.Background(), productEvent())

	requireSyncFailed(t, result, err)
	requireCalls(t, calls, 2)
	requireDLQReason(t, dlq, "Title is required")
}

func shopifyCredentialMarkers() []string {
	return []string{
		"shpat_",
		"shpca_",
		"shpss_",
		"shpua_",
		"myshopify.com",
		"X-Shopify-Access-Token",
	}
}

func scanFixtureFile(path string, markers []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return scanFixtureContent(path, string(data), markers)
}

func scanFixtureContent(path, content string, markers []string) error {
	for _, marker := range markers {
		if strings.Contains(content, marker) {
			return errors.New("credential marker found in " + path + ": " + marker)
		}
	}
	return nil
}

func userErrorServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"productSet": map[string]any{
					"product": nil,
					"userErrors": []map[string]any{
						{"field": []string{"input", "title"}, "message": "Title is required"},
					},
				},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func mustTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(Config{BaseURL: server.URL, AccessToken: "token-test"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func mustTestEngine(t *testing.T, client *Client, dlq *marketplacesync.InMemoryDLQ) *marketplacesync.Engine {
	t.Helper()
	engine, err := marketplacesync.NewEngine(marketplacesync.EngineConfig{
		Connector:   client,
		Ledger:      marketplacesync.NewInMemoryLedger(),
		DLQ:         dlq,
		MaxAttempts: 2,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return engine
}

func requireSyncFailed(t *testing.T, result marketplacesync.SyncResult, err error) {
	t.Helper()
	if !errors.Is(err, marketplacesync.ErrSyncFailed) {
		t.Fatalf("error = %v, want ErrSyncFailed", err)
	}
	if result.Status != marketplacesync.StatusDLQ {
		t.Fatalf("status = %s, want dlq", result.Status)
	}
}

func requireCalls(t *testing.T, calls *int, want int) {
	t.Helper()
	if *calls != want {
		t.Fatalf("calls = %d, want %d retry attempts", *calls, want)
	}
}

func requireDLQReason(t *testing.T, dlq *marketplacesync.InMemoryDLQ, want string) {
	t.Helper()
	records := dlq.Records()
	if len(records) != 1 {
		t.Fatalf("dlq records = %d, want 1", len(records))
	}
	if !strings.Contains(records[0].Reason, want) {
		t.Fatalf("dlq reason = %q", records[0].Reason)
	}
}

package shopee

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

func TestQANewClientRejectsLiveShopeeBaseURLByDefault(t *testing.T) {
	t.Parallel()

	_, err := NewClient(Config{
		BaseURL:     "https://partner.shopeemobile.com",
		PartnerID:   123456,
		PartnerKey:  testPartnerKey,
		AccessToken: "test-access-token",
		ShopID:      987654,
	}, nil)
	if !errors.Is(err, ErrLiveCallsDisabled) {
		t.Fatalf("err = %v, want ErrLiveCallsDisabled", err)
	}
}

func TestQANewClientAllowsLiveShopeeBaseURLOnlyWhenExplicit(t *testing.T) {
	t.Parallel()

	client, err := NewClient(Config{
		BaseURL:          "https://partner.shopeemobile.com",
		PartnerID:        123456,
		PartnerKey:       testPartnerKey,
		AccessToken:      "test-access-token",
		ShopID:           987654,
		AllowLiveBaseURL: true,
	}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestQANewClientUsesBoundedDefaultHTTPTimeout(t *testing.T) {
	t.Parallel()

	client, err := NewClient(Config{
		BaseURL:     "https://example.invalid",
		PartnerID:   123456,
		PartnerKey:  testPartnerKey,
		AccessToken: "test-access-token",
		ShopID:      987654,
	}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.httpClient == nil {
		t.Fatal("http client is nil")
	}
	if client.httpClient.Timeout <= 0 || client.httpClient.Timeout > 30*time.Second {
		t.Fatalf("default timeout = %s, want bounded positive timeout <= 30s", client.httpClient.Timeout)
	}
}

func TestQAFixturesContainNoShopeeCredentialMarkers(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "product_upsert_success.json")
	if err := scanFixtureFile(path, shopeeCredentialMarkers()); err != nil {
		t.Fatal(err)
	}
}

func TestQAShopeeAPIErrorsFlowThroughSharedEngineToDLQ(t *testing.T) {
	t.Parallel()

	server, calls := shopeeErrorServer(t)
	client := mustServerClient(t, server, fixedClock(1777777811))
	dlq := marketplacesync.NewInMemoryDLQ()
	engine := mustTestEngine(t, client, dlq)

	result, err := engine.Sync(context.Background(), productEvent())

	requireSyncFailed(t, result, err)
	requireCalls(t, calls, 2)
	requireDLQReason(t, dlq, "invalid item name")
}

func shopeeCredentialMarkers() []string {
	return []string{
		"partner.shopeemobile.com",
		"shopee.com",
		"shopeemobile.com",
		"access_token",
		"partner_id",
		"shop_id",
		"sign",
	}
}

func scanFixtureFile(path string, markers []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, marker := range markers {
		if strings.Contains(string(data), marker) {
			return errors.New("credential marker found in " + path + ": " + marker)
		}
	}
	return nil
}

func shopeeErrorServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   "error_param",
			"message": "invalid item name",
		})
	}))
	t.Cleanup(server.Close)
	return server, &calls
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

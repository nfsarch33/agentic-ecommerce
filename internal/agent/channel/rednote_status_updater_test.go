// File scope: v3.9.0 carry-forward closure -- RedNoteStatusUpdater
// RED tests.
package channel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/fulfilment"
)

func mustRedNoteClient(t *testing.T, bridgeURL string) *RedNoteUIAutoClient {
	t.Helper()
	client, err := NewRedNoteUIAutoClient(nil, RedNoteUIAutoClientConfig{
		BridgeURL:    bridgeURL,
		BridgeSecret: []byte("0123456789abcdef0123456789abcdef"),
		TenantID:     "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewRedNoteUIAutoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	return client
}

func TestRedNoteStatusUpdater_PostsToBridge(t *testing.T) {
	t.Parallel()
	var seenPath, seenSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenSig = r.Header.Get("X-Bridge-Sign")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	client := mustRedNoteClient(t, srv.URL)
	updater, err := NewRedNoteStatusUpdater(nil, client, "tenant-1")
	if err != nil {
		t.Fatalf("NewRedNoteStatusUpdater: %v", err)
	}
	if updater.ChannelName() != RedNoteChannelName {
		t.Fatalf("expected rednote channel, got %s", updater.ChannelName())
	}
	err = updater.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{
		TenantID:        "tenant-1",
		ExternalOrderID: "ord-9",
		Status:          "shipped",
		TrackingNumber:  "AP-9",
		DeliveryDate:    time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("UpdateOrderStatus: %v", err)
	}
	if seenPath != RedNoteStatusBridgePath {
		t.Fatalf("expected bridge status path %s, got %s", RedNoteStatusBridgePath, seenPath)
	}
	if seenSig == "" {
		t.Fatalf("expected X-Bridge-Sign header set")
	}
}

func TestRedNoteStatusUpdater_BridgeRejects(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()
	client := mustRedNoteClient(t, srv.URL)
	updater, _ := NewRedNoteStatusUpdater(nil, client, "tenant-1")
	err := updater.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{
		TenantID:        "tenant-1",
		ExternalOrderID: "ord-9",
		Status:          "shipped",
	})
	if !errors.Is(err, ErrRedNoteBridgeRejected) {
		t.Fatalf("expected ErrRedNoteBridgeRejected, got %v", err)
	}
}

func TestRedNoteStatusUpdater_CloseDelegates(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	client := mustRedNoteClient(t, srv.URL)
	updater, _ := NewRedNoteStatusUpdater(nil, client, "tenant-1")
	if err := updater.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestRedNoteStatusUpdater_NilClientRejected(t *testing.T) {
	t.Parallel()
	if _, err := NewRedNoteStatusUpdater(nil, nil, "tenant-1"); !errors.Is(err, ErrRedNoteUnconfigured) {
		t.Fatalf("expected ErrRedNoteUnconfigured, got %v", err)
	}
}

func TestRedNoteStatusUpdater_TenantRequired(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	client := mustRedNoteClient(t, srv.URL)
	if _, err := NewRedNoteStatusUpdater(nil, client, ""); !errors.Is(err, ErrRedNoteUnconfigured) {
		t.Fatalf("expected ErrRedNoteUnconfigured, got %v", err)
	}
}

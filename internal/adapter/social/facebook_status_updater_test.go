// File scope: v3.9.0 carry-forward closure -- FacebookStatusUpdater
// RED tests.
package social

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/fulfilment"
)

func TestFacebookStatusUpdater_NilClientRejected(t *testing.T) {
	t.Parallel()
	if _, err := NewFacebookStatusUpdater(nil, nil, "tenant-1"); !errors.Is(err, ErrFacebookUnconfigured) {
		t.Fatalf("expected ErrFacebookUnconfigured, got %v", err)
	}
}

func TestFacebookStatusUpdater_TenantRequired(t *testing.T) {
	t.Parallel()
	client := mustFacebookClient(t)
	defer client.Close(context.Background())
	if _, err := NewFacebookStatusUpdater(nil, client, ""); !errors.Is(err, ErrFacebookUnconfigured) {
		t.Fatalf("expected ErrFacebookUnconfigured, got %v", err)
	}
}

func TestFacebookStatusUpdater_ChannelName(t *testing.T) {
	t.Parallel()
	client := mustFacebookClient(t)
	defer client.Close(context.Background())
	updater, err := NewFacebookStatusUpdater(nil, client, "tenant-1")
	if err != nil {
		t.Fatalf("NewFacebookStatusUpdater: %v", err)
	}
	if updater.ChannelName() != FacebookChannelName {
		t.Fatalf("expected facebook channel name, got %s", updater.ChannelName())
	}
	var _ fulfilment.ChannelStatusUpdater = updater
}

func TestFacebookStatusUpdater_CloseDelegates(t *testing.T) {
	t.Parallel()
	client := mustFacebookClient(t)
	updater, _ := NewFacebookStatusUpdater(nil, client, "tenant-1")
	if err := updater.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestFacebookStatusUpdater_MissingExternalOrderID(t *testing.T) {
	t.Parallel()
	client := mustFacebookClient(t)
	defer client.Close(context.Background())
	updater, _ := NewFacebookStatusUpdater(nil, client, "tenant-1")
	err := updater.UpdateOrderStatus(context.Background(), fulfilment.ChannelStatusUpdate{
		TenantID: "tenant-1",
		Status:   "shipped",
	})
	if !errors.Is(err, ErrFacebookUnconfigured) {
		t.Fatalf("expected ErrFacebookUnconfigured, got %v", err)
	}
}

func mustFacebookClient(t *testing.T) *FacebookShopClient {
	t.Helper()
	store := NewFacebookMemoryTokenStore()
	tm, err := NewFacebookTokenManager(FacebookTokenManagerConfig{
		Store:     store,
		Exchanger: facebookStubExchanger{},
	})
	if err != nil {
		t.Fatalf("NewFacebookTokenManager: %v", err)
	}
	client, err := NewFacebookShopClient(nil, FacebookShopConfig{
		TokenManager: tm,
		AppID:        "app-1",
		AppSecret:    []byte("0123456789abcdef0123456789abcdef"),
		CatalogueID:  "cat-1",
		TenantID:     "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewFacebookShopClient: %v", err)
	}
	return client
}

type facebookStubExchanger struct{}

func (facebookStubExchanger) Exchange(_ context.Context, req FacebookOAuthBootstrapRequest) (FacebookToken, error) {
	return FacebookToken{TenantID: req.TenantID, AccessToken: "x"}, nil
}

func (facebookStubExchanger) Refresh(_ context.Context, _ FacebookToken) (FacebookToken, error) {
	return FacebookToken{}, nil
}

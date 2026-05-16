package marketplacesync

import (
	"context"
	"testing"
)

func TestRouterSyncRoutesByProvider(t *testing.T) {
	t.Parallel()

	shopifyConnector := &routerRecordingConnector{remoteID: "shopify-remote"}
	shopeeConnector := &routerRecordingConnector{remoteID: "shopee-remote"}
	router, err := NewRouter(RouterConfig{
		Connectors: map[string]Connector{
			"shopify": shopifyConnector,
			"shopee":  shopeeConnector,
		},
		Ledger:      NewInMemoryLedger(),
		DLQ:         NewInMemoryDLQ(),
		Metrics:     newRecordingMetrics(),
		MaxAttempts: 2,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	shopifyResult, err := router.Sync(context.Background(), ProductEvent{
		TenantID:   "tenant-a",
		Provider:   "shopify",
		EntityType: EntityProduct,
		EntityID:   "sku-shopify",
		Operation:  OperationUpsert,
		Version:    "v1",
	})
	if err != nil {
		t.Fatalf("shopify sync: %v", err)
	}
	if shopifyResult.Status != StatusApplied || shopifyConnector.calls != 1 || shopeeConnector.calls != 0 {
		t.Fatalf("shopify result = %+v, calls shopify=%d shopee=%d", shopifyResult, shopifyConnector.calls, shopeeConnector.calls)
	}

	shopeeResult, err := router.Sync(context.Background(), ProductEvent{
		TenantID:   "tenant-a",
		Provider:   "shopee",
		EntityType: EntityProduct,
		EntityID:   "sku-shopee",
		Operation:  OperationUpsert,
		Version:    "v2",
	})
	if err != nil {
		t.Fatalf("shopee sync: %v", err)
	}
	if shopeeResult.Status != StatusApplied || shopifyConnector.calls != 1 || shopeeConnector.calls != 1 {
		t.Fatalf("shopee result = %+v, calls shopify=%d shopee=%d", shopeeResult, shopifyConnector.calls, shopeeConnector.calls)
	}
}

type routerRecordingConnector struct {
	calls    int
	remoteID string
}

func (c *routerRecordingConnector) Apply(_ context.Context, _ ProductEvent) (ApplyResult, error) {
	c.calls++
	return ApplyResult{RemoteID: c.remoteID}, nil
}

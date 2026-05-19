package main

import (
	"context"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/adapter/woocommerce"
)

// File scope: smoke coverage for noopChannel.ListProducts which sat at
// 0% but must satisfy the WooCommerceClient interface for dry-run mode
// to compile cleanly.

func TestNoopChannelListProductsReturnsEmptyResult(t *testing.T) {
	t.Parallel()

	got, err := noopChannel{}.ListProducts(context.Background(), woocommerce.ListOptions{})
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if got != nil {
		t.Fatalf("ListProducts = %v, want nil for noop dry-run", got)
	}
}

func TestNoopChannelHonoursListOptions(t *testing.T) {
	t.Parallel()

	got, err := noopChannel{}.ListProducts(context.Background(), woocommerce.ListOptions{Page: 5, PerPage: 25})
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListProducts result = %d, want 0 (noop)", len(got))
	}
}

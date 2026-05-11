//go:build integration_pg

// File scope: v6.1.0 CF-15 -- testcontainers-driven integration
// coverage for the Postgres-backed webhook IdempotencyStore.
//
// Gated behind the `integration_pg` build tag so the default
// `go test ./...` lane keeps the unit-only runtime profile.
package postgres_test

import (
	"context"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/postgres"
)

// TestIntegrationWebhookIdempotencyReserveFirstObservation
// exercises the full INSERT ... ON CONFLICT DO NOTHING RETURNING
// path against a real Postgres so the SQL is validated by the
// planner, not just by the unit-level fake.
func TestIntegrationWebhookIdempotencyReserveFirstObservation(t *testing.T) {
	pool := startContainerPool(t)
	ctx := context.Background()

	store := postgres.NewWebhookIdempotencyStore(pool)
	ok, err := store.Reserve(ctx, "tenant-int-first", "key-1")
	if err != nil {
		t.Fatalf("Reserve first: %v", err)
	}
	if !ok {
		t.Fatal("Reserve first: got false, want true")
	}
}

// TestIntegrationWebhookIdempotencyDuplicateReserveReturnsFalse
// pins the at-least-once delivery guard end-to-end: the second
// Reserve for the same (tenant, key) must return false with no
// error.
func TestIntegrationWebhookIdempotencyDuplicateReserveReturnsFalse(t *testing.T) {
	pool := startContainerPool(t)
	ctx := context.Background()

	store := postgres.NewWebhookIdempotencyStore(pool)
	if _, err := store.Reserve(ctx, "tenant-int-dup", "key-2"); err != nil {
		t.Fatalf("Reserve seed: %v", err)
	}
	ok, err := store.Reserve(ctx, "tenant-int-dup", "key-2")
	if err != nil {
		t.Fatalf("Reserve dup: %v", err)
	}
	if ok {
		t.Fatal("Reserve dup: got true, want false (ON CONFLICT DO NOTHING)")
	}
}

// TestIntegrationWebhookIdempotencyTenantIsolation verifies the
// same idempotency key under two different tenants reserves
// independently.
func TestIntegrationWebhookIdempotencyTenantIsolation(t *testing.T) {
	pool := startContainerPool(t)
	ctx := context.Background()

	store := postgres.NewWebhookIdempotencyStore(pool)
	const sharedKey = "key-shared"
	ok1, err := store.Reserve(ctx, "tenant-iso-A", sharedKey)
	if err != nil || !ok1 {
		t.Fatalf("Reserve tenant A: ok=%v err=%v", ok1, err)
	}
	ok2, err := store.Reserve(ctx, "tenant-iso-B", sharedKey)
	if err != nil || !ok2 {
		t.Fatalf("Reserve tenant B: ok=%v err=%v (each tenant must observe the key as new)", ok2, err)
	}
}

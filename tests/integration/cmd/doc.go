//go:build integration_pg

// Package cmdintegration hosts the v6.1.0 Story 2 testcontainers-go
// integration harness for the cmd/* binaries.
//
// Each cmd entry point (cmd/mc-api, cmd/temporal-worker, cmd/ec-cli,
// cmd/agent-worker) ships with strong unit-level coverage already
// (80-86% per the v6.0.0 baseline), so this harness focuses on the
// cross-boundary contracts that unit tests cannot exercise: real
// Postgres connection establishment, migration application, and
// composition-root wiring against an ephemeral container.
//
// Gated behind the `integration_pg` build tag so the default
// `go test ./...` lane keeps its hermetic profile. Run via:
//
//	runx make integration-pg --repo ecommerce
package cmdintegration

//go:build chaos

// Package chaos contains v2.10.1 resilience-validation tests that
// exercise the v2.10.0 lifecycle / workerpool / memwatch surfaces
// against deliberately injected failures.
//
// These tests are gated behind the `chaos` build tag because they
//
//   - depend on Docker via testcontainers-go;
//   - take 30 s -- 3 min each (vs. <1 s for unit tests);
//   - exercise OS-level signals and network namespace isolation that
//     do not belong on every-PR CI.
//
// Run locally: `go test -tags chaos -race ./tests/chaos/...`
// CI: weekly nightly job + on-demand `[chaos]` PR-trigger label.
//
// Each test self-skips with a clear message when Docker is unreachable
// or DISABLE_DOCKER_TESTCONTAINERS=1 is set, so the whole suite stays
// hermetic on dev machines without Docker.
//
// File ledger:
//
//	oom_test.go            -- HeapCeiling + lifecycle.Manager critical-shutdown path
//	postgres_flap_test.go  -- Stop/Start tcpostgres container + pool recovery
//	redis_flap_test.go     -- Stop/Start generic redis container + adapter recovery
//	temporal_flap_test.go  -- Stop/Start generic temporal-frontend container + worker recovery
package chaos

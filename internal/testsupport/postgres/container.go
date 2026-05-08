//go:build integration_pg

// Package testsupportpg centralises the testcontainers-driven postgres
// fixture used by integration_pg-tagged tests. Before this package every
// caller duplicated container start, migration loading, and pool
// teardown. The shared helper enforces:
//
//   - one canonical migration list (in migration_files.go) so a new
//     migration only has to be appended once;
//   - one wait strategy (database-system-ready log + pool ping);
//   - automatic skip when DISABLE_DOCKER_TESTCONTAINERS=1 or Docker is
//     unreachable, so the default `go test ./...` path stays hermetic;
//   - automatic teardown via t.Cleanup so leaks cannot accumulate
//     across test binaries.
//
// The helper is intentionally narrow: it returns a *pgxpool.Pool. Each
// test owns whatever schema isolation it needs (e.g. CREATE SCHEMA
// test_<random> via SetSearchPath). This keeps the shared fixture small
// and lets callers compose it into more elaborate scenarios.
//
// The Docker-dependent code lives behind the integration_pg build tag
// so it is excluded from the default `go test ./...` coverage profile.
// The pure helpers (migration filename ledger, runtime.Caller path
// resolver) live in paths.go and migration_files.go without the build
// tag so they remain unit-tested.
package testsupportpg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	defaultPostgresImage    = "postgres:16-alpine"
	defaultDatabase         = "ecommerce_test"
	defaultUser             = "ecommerce"
	defaultPassword         = "ecommerce"
	defaultStartupTimeout   = 90 * time.Second
	defaultReadinessTimeout = 60 * time.Second
)

// Options tunes the shared postgres container fixture. Defaults match
// the canonical adapter test suite; tests should only override values
// when they have a reason to (e.g. raising the startup timeout on slow
// CI runners).
type Options struct {
	// Image overrides the postgres image tag. Default: postgres:16-alpine.
	Image string
	// Database overrides the bootstrap database name.
	Database string
	// User overrides the bootstrap superuser name.
	User string
	// Password overrides the bootstrap superuser password.
	Password string
	// MigrationFiles is the ordered list of *.up.sql files to apply via
	// tcpostgres.WithInitScripts. Empty means "apply the canonical
	// migration set".
	MigrationFiles []string
	// StartupTimeout overrides the Docker container startup timeout.
	StartupTimeout time.Duration
	// ReadinessTimeout overrides the postgres-ready wait strategy
	// timeout.
	ReadinessTimeout time.Duration
}

// StartPool boots a per-test postgres container, applies the canonical
// migrations, and returns a ready-to-use *pgxpool.Pool. The pool is
// automatically closed and the container terminated via t.Cleanup. If
// Docker is unavailable or DISABLE_DOCKER_TESTCONTAINERS=1, the test is
// skipped so unit-only `go test ./...` stays green on CI runners
// without Docker.
//
// The function intentionally does NOT memoise across tests: each call
// produces a fresh container so failures are deterministic and one
// flaky test cannot poison its neighbours. If a test binary has many
// integration tests, batch them in a single TestMain or use one
// package-level container with t.Run subtests.
func StartPool(t *testing.T, opts Options) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DISABLE_DOCKER_TESTCONTAINERS") == "1" {
		t.Skip("DISABLE_DOCKER_TESTCONTAINERS=1; skipping testcontainers postgres fixture")
	}

	o := opts
	if o.Image == "" {
		o.Image = defaultPostgresImage
	}
	if o.Database == "" {
		o.Database = defaultDatabase
	}
	if o.User == "" {
		o.User = defaultUser
	}
	if o.Password == "" {
		o.Password = defaultPassword
	}
	if o.StartupTimeout <= 0 {
		o.StartupTimeout = defaultStartupTimeout
	}
	if o.ReadinessTimeout <= 0 {
		o.ReadinessTimeout = defaultReadinessTimeout
	}
	if len(o.MigrationFiles) == 0 {
		o.MigrationFiles = CanonicalMigrationFiles()
	}

	migrationDir, err := ResolveMigrationDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	migrationPaths := make([]string, 0, len(o.MigrationFiles))
	for _, name := range o.MigrationFiles {
		migrationPaths = append(migrationPaths, filepath.Join(migrationDir, name))
	}

	ctx, cancel := context.WithTimeout(context.Background(), o.StartupTimeout)
	defer cancel()

	container, err := tcpostgres.Run(
		ctx,
		o.Image,
		tcpostgres.WithDatabase(o.Database),
		tcpostgres.WithUsername(o.User),
		tcpostgres.WithPassword(o.Password),
		tcpostgres.WithInitScripts(migrationPaths...),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("testcontainers postgres unavailable (likely no Docker): %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	if err := container.Start(ctx); err != nil && !errors.Is(err, errAlreadyStarted) && err.Error() != errAlreadyStarted.Error() {
		t.Skipf("postgres container failed to start: %v", err)
	}

	if err := waitForReady(ctx, container, o.ReadinessTimeout); err != nil {
		t.Skipf("postgres readiness wait failed: %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("postgres ping failed: %v", err)
	}
	return pool
}

// errAlreadyStarted matches the testcontainers-go error returned when
// Run() already started the container before Start() was called. We
// compare via .Error() string because the upstream package does not
// export the sentinel.
var errAlreadyStarted = errors.New("container is already started")

func waitForReady(ctx context.Context, container *tcpostgres.PostgresContainer, timeout time.Duration) error {
	strat := wait.ForLog("database system is ready to accept connections").WithStartupTimeout(timeout)
	return strat.WaitUntilReady(ctx, container)
}

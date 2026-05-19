//go:build integration_pg

// File scope: v6.2.1 QA Story 3 -- testcontainers-go Postgres-backed
// JWT rotation grace-period E2E test.
//
// Drives migration 0038_jwt_key_versions, persists a v0 -> v1 -> v2
// rotation ring, builds two independent *jwt.Rotator instances
// (representing two mc-api replicas) from the same DB snapshot, and
// asserts:
//
//  1. v1 tokens minted by replica A verify on replica B until v1.NotAfter.
//  2. v0 tokens (retired key) are rejected on both replicas with
//     ErrExpiredKey, regardless of which replica minted them.
//  3. After v1.NotAfter passes, both replicas reject v1 tokens with
//     ErrExpiredKey -- the grace boundary is enforced consistently.
//
// no-shell-leak: the test resolves the Postgres host via the
// testcontainers ConnectionString surface; no raw IPs in argv.
package v621

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

	authjwt "github.com/nfsarch33/helixon-ec/internal/auth/jwt"
)

const (
	pgJWTImage    = "postgres:16-alpine"
	pgJWTDatabase = "ecommerce_jwt_v621"
	pgJWTUser     = "ecommerce"
	pgJWTPassword = "ecommerce"

	jwtSecretV0 = "v0-secret-32-bytes-padding-aaaaaa"
	jwtSecretV1 = "v1-secret-32-bytes-padding-bbbbbb"
	jwtSecretV2 = "v2-secret-32-bytes-padding-cccccc"
)

// secretRefs is the deterministic secret-resolver the test installs
// in place of the production 1Password / Secrets Manager loader.
// Mirrors the v6.2.0 migration comment that secret bytes live OUTSIDE
// jwt_key_versions and resolve at boot.
var secretRefs = map[string][]byte{
	"vault:v0": []byte(jwtSecretV0),
	"vault:v1": []byte(jwtSecretV1),
	"vault:v2": []byte(jwtSecretV2),
}

func TestV621_JWTRotationGracePeriodE2E(t *testing.T) {
	if os.Getenv("DISABLE_DOCKER_TESTCONTAINERS") == "1" {
		t.Skip("DISABLE_DOCKER_TESTCONTAINERS=1; skipping testcontainers integration test")
	}
	migration := resolveJWTMigrationPath(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	container, err := tcpostgres.Run(
		ctx,
		pgJWTImage,
		tcpostgres.WithDatabase(pgJWTDatabase),
		tcpostgres.WithUsername(pgJWTUser),
		tcpostgres.WithPassword(pgJWTPassword),
		tcpostgres.WithInitScripts(migration),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("testcontainers postgres unavailable (likely no Docker): %v", err)
	}
	defer func() { _ = container.Terminate(context.Background()) }()

	strat := wait.ForLog("database system is ready to accept connections").WithStartupTimeout(60 * time.Second)
	if err := strat.WaitUntilReady(ctx, container); err != nil {
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
	defer pool.Close()

	// --- Seed the rotation ring -------------------------------------
	// v0 is retired (not_after in the past). v1 is active (not_after
	// in the future). v2 is pending (not_after null, not active yet).
	wallClock := time.Now().UTC().Truncate(time.Second)
	v0NotAfter := wallClock.Add(-1 * time.Hour)
	v1NotAfter := wallClock.Add(2 * time.Hour)

	seedKeyVersion(t, ctx, pool, "v0", "retired", "vault:v0", &v0NotAfter)
	seedKeyVersion(t, ctx, pool, "v1", "active", "vault:v1", &v1NotAfter)
	seedKeyVersion(t, ctx, pool, "v2", "pending", "vault:v2", nil)

	// --- Materialise two replicas from the same DB snapshot ----------
	keys := loadKeysFromDB(t, ctx, pool, wallClock)
	replicaA := newRotatorFromDB(t, keys, "v1", wallClock)
	replicaB := newRotatorFromDB(t, keys, "v1", wallClock)

	// (1) v1 token minted on replica A verifies on replica B inside grace.
	tokenV1, err := replicaA.Mint(authjwt.MintRequest{Subject: "ops@example.com"})
	if err != nil {
		t.Fatalf("replicaA.Mint(v1): %v", err)
	}
	if _, err := replicaB.Verify(tokenV1); err != nil {
		t.Fatalf("replicaB.Verify(v1) inside grace err=%v want nil", err)
	}

	// (2) Mint a v0 token directly (bypassing the active-key path) and
	// confirm both replicas reject it because v0 is retired.
	tokenV0 := mintWithKey(t, wallClock, "v0", []byte(jwtSecretV0))
	for name, r := range map[string]*authjwt.Rotator{"replicaA": replicaA, "replicaB": replicaB} {
		if _, err := r.Verify(tokenV0); !errors.Is(err, authjwt.ErrExpiredKey) {
			t.Fatalf("%s.Verify(v0 retired) err=%v want ErrExpiredKey", name, err)
		}
	}

	// (3) Roll time past v1.NotAfter. Both replicas (which share the
	// same Now function injected per replica) must now reject v1.
	postV1 := v1NotAfter.Add(time.Second)
	replicaAPost := newRotatorFromDB(t, keys, "v1", postV1)
	replicaBPost := newRotatorFromDB(t, keys, "v1", postV1)
	for name, r := range map[string]*authjwt.Rotator{"replicaA": replicaAPost, "replicaB": replicaBPost} {
		if _, err := r.Verify(tokenV1); !errors.Is(err, authjwt.ErrExpiredKey) {
			t.Fatalf("%s.Verify(v1 after grace) err=%v want ErrExpiredKey", name, err)
		}
	}
}

func seedKeyVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, version, state, secretRef string, notAfter *time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx,
		`INSERT INTO jwt_key_versions (version, state, secret_ref, not_after, activated_at)
		 VALUES ($1, $2, $3, $4, CASE WHEN $2 = 'active' THEN now() ELSE NULL END)
		 ON CONFLICT (version) DO UPDATE
		   SET state = EXCLUDED.state,
		       secret_ref = EXCLUDED.secret_ref,
		       not_after = EXCLUDED.not_after`,
		version, state, secretRef, notAfter,
	)
	if err != nil {
		t.Fatalf("seed %s: %v", version, err)
	}
}

func loadKeysFromDB(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) []authjwt.Key {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT version, secret_ref, not_after
		 FROM jwt_key_versions
		 WHERE state IN ('pending', 'active', 'retiring', 'retired')
		 ORDER BY version`,
	)
	if err != nil {
		t.Fatalf("load keys: %v", err)
	}
	defer rows.Close()

	var out []authjwt.Key
	for rows.Next() {
		var (
			version   string
			secretRef string
			notAfter  *time.Time
		)
		if err := rows.Scan(&version, &secretRef, &notAfter); err != nil {
			t.Fatalf("scan: %v", err)
		}
		secret, ok := secretRefs[secretRef]
		if !ok {
			t.Fatalf("missing secret for ref=%s", secretRef)
		}
		key := authjwt.Key{Version: version, Secret: secret}
		if notAfter != nil {
			key.NotAfter = notAfter.UTC()
		}
		out = append(out, key)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("loadKeysFromDB returned empty ring")
	}
	return out
}

func newRotatorFromDB(t *testing.T, keys []authjwt.Key, active string, now time.Time) *authjwt.Rotator {
	t.Helper()
	r, err := authjwt.NewRotator(authjwt.Config{
		Keys:          keys,
		ActiveVersion: active,
		Issuer:        "agentic-ecommerce",
		Audience:      "mc-api",
		AccessTTL:     5 * time.Minute,
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	return r
}

// mintWithKey is a test helper that produces a token against an
// arbitrary key version, bypassing the active-key path so the test
// can exercise the "v0 retired but still in the ring" guard.
func mintWithKey(t *testing.T, now time.Time, version string, secret []byte) string {
	t.Helper()
	r, err := authjwt.NewRotator(authjwt.Config{
		Keys:          []authjwt.Key{{Version: version, Secret: secret}},
		ActiveVersion: version,
		Issuer:        "agentic-ecommerce",
		Audience:      "mc-api",
		AccessTTL:     5 * time.Minute,
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("mintWithKey NewRotator(%s): %v", version, err)
	}
	tok, err := r.Mint(authjwt.MintRequest{Subject: "ops@example.com"})
	if err != nil {
		t.Fatalf("mintWithKey Mint(%s): %v", version, err)
	}
	return tok
}

func resolveJWTMigrationPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations", "0038_jwt_key_versions.up.sql"))
	if err != nil {
		t.Fatalf("resolve migration path: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("stat migration: %v", err)
	}
	return p
}

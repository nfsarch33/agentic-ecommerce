package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDatabasePoolConfigFromEnvAppliesCloudTuning(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_POOL_MAX_CONNS", "32")
	t.Setenv("ECOMMERCE_DB_POOL_MIN_CONNS", "4")
	t.Setenv("ECOMMERCE_DB_POOL_MAX_CONN_LIFETIME", "45m")
	t.Setenv("ECOMMERCE_DB_POOL_MAX_CONN_IDLE_TIME", "7m")
	t.Setenv("ECOMMERCE_DB_CONNECT_TIMEOUT", "3s")

	cfg, err := databasePoolConfigFromEnv("postgres://ecommerce:secret@db.internal:5432/ecommerce?sslmode=require")
	if err != nil {
		t.Fatalf("databasePoolConfigFromEnv: %v", err)
	}
	if cfg.MaxConns != 32 {
		t.Fatalf("MaxConns = %d, want 32", cfg.MaxConns)
	}
	if cfg.MinConns != 4 {
		t.Fatalf("MinConns = %d, want 4", cfg.MinConns)
	}
	if cfg.MaxConnLifetime != 45*time.Minute {
		t.Fatalf("MaxConnLifetime = %s, want 45m", cfg.MaxConnLifetime)
	}
	if cfg.MaxConnIdleTime != 7*time.Minute {
		t.Fatalf("MaxConnIdleTime = %s, want 7m", cfg.MaxConnIdleTime)
	}
	if cfg.ConnConfig.ConnectTimeout != 3*time.Second {
		t.Fatalf("ConnectTimeout = %s, want 3s", cfg.ConnConfig.ConnectTimeout)
	}
}

func TestDatabasePoolConfigFromEnvClampsMinConns(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_POOL_MAX_CONNS", "3")
	t.Setenv("ECOMMERCE_DB_POOL_MIN_CONNS", "8")

	cfg, err := databasePoolConfigFromEnv("postgres://ecommerce:secret@db.internal:5432/ecommerce?sslmode=require")
	if err != nil {
		t.Fatalf("databasePoolConfigFromEnv: %v", err)
	}
	if cfg.MaxConns != 3 || cfg.MinConns != 3 {
		t.Fatalf("pool conns = max:%d min:%d, want max/min 3", cfg.MaxConns, cfg.MinConns)
	}
}

func TestReadyzTimeoutReportsDegradedDetailWithoutSecrets(t *testing.T) {
	srv, _ := testServerWithCfg(t, serverConfig{readinessTimeout: 10 * time.Millisecond})
	srv.readiness = []readinessProbe{
		{
			name:     "database",
			optional: false,
			check: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	var got readyzResponse
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&got); err != nil {
		t.Fatalf("decode readyz response: %v", err)
	}
	check := got.Checks["database"]
	if check.Status != "fail" || check.Error != "timeout" || check.Detail != "dependency_timeout" {
		t.Fatalf("database check = %+v, want timeout detail", check)
	}
	for _, forbidden := range []string{"secret", "postgres://", "db.internal"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("readyz leaked %q in body: %s", forbidden, body)
		}
	}
}

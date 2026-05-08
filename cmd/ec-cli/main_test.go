package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestDeps(env map[string]string) (appDeps, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	deps := appDeps{
		stdout: stdout,
		stderr: stderr,
		getenv: func(k string) string { return env[k] },
		now:    func() string { return "2026-05-09T00:00:00Z" },
	}
	return deps, stdout, stderr
}

func TestRunAppPrintsUsageWhenNoSubcommand(t *testing.T) {
	t.Parallel()
	deps, _, stderr := newTestDeps(nil)
	if got := runApp(context.Background(), []string{"ec-cli"}, deps); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr should contain Usage:, got %q", stderr.String())
	}
}

func TestRunAppHelpReturnsZero(t *testing.T) {
	t.Parallel()
	deps, stdout, _ := newTestDeps(nil)
	if got := runApp(context.Background(), []string{"ec-cli", "help"}, deps); got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}
	if !strings.Contains(stdout.String(), "Subcommands:") {
		t.Fatalf("expected Subcommands listing in stdout")
	}
}

func TestRunAppUnknownSubcommand(t *testing.T) {
	t.Parallel()
	deps, _, stderr := newTestDeps(nil)
	if got := runApp(context.Background(), []string{"ec-cli", "kaboom"}, deps); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("expected unknown subcommand message, got %q", stderr.String())
	}
}

func TestRunVersionPrintsMetadata(t *testing.T) {
	t.Parallel()
	deps, stdout, _ := newTestDeps(nil)
	if got := runApp(context.Background(), []string{"ec-cli", "version"}, deps); got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}
	if !strings.Contains(stdout.String(), "ec-cli") {
		t.Fatalf("expected version banner")
	}
	if !strings.Contains(stdout.String(), "version") || !strings.Contains(stdout.String(), "commit") {
		t.Fatalf("expected version + commit fields")
	}
}

func TestDoctorMissingEnvFailsRequiredCheck(t *testing.T) {
	t.Parallel()
	deps, stdout, _ := newTestDeps(map[string]string{})
	exit := runApp(context.Background(), []string{"ec-cli", "doctor", "--timeout-seconds=1"}, deps)
	if exit == 0 {
		t.Fatalf("expected non-zero exit for missing env")
	}
	if !strings.Contains(stdout.String(), "UNHEALTHY") {
		t.Fatalf("expected UNHEALTHY verdict in output, got %q", stdout.String())
	}
}

func TestDoctorJSONOutputShape(t *testing.T) {
	t.Parallel()
	deps, stdout, _ := newTestDeps(map[string]string{})
	_ = runApp(context.Background(), []string{"ec-cli", "doctor", "--json", "--timeout-seconds=1"}, deps)
	var report doctorReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("expected JSON output, got %q (%v)", stdout.String(), err)
	}
	if len(report.Checks) == 0 {
		t.Fatalf("expected at least one check in report, got 0")
	}
	if report.Generated == "" {
		t.Fatalf("expected generated timestamp")
	}
}

func TestDoctorEnvCheckPassesWhenAllSet(t *testing.T) {
	t.Parallel()
	deps, _, _ := newTestDeps(map[string]string{
		"ECOMMERCE_DB_URL":        "postgres://x:y@example.com:5432/db",
		"ECOMMERCE_REDIS_URL":     "redis://example.com:6379",
		"ECOMMERCE_TEMPORAL_ADDR": "example.com:7233",
	})
	check := checkRequiredEnv(deps)
	if !check.OK {
		t.Fatalf("expected env check to pass, got %+v", check)
	}
}

func TestHostPortFromPostgresDSN(t *testing.T) {
	t.Parallel()
	cases := []struct {
		dsn  string
		host string
		port string
		ok   bool
	}{
		{dsn: "postgres://u:p@db.example.com:5433/x", host: "db.example.com", port: "5433", ok: true},
		{dsn: "postgres://db.example.com/x", host: "db.example.com", port: "5432", ok: true},
		{dsn: "host=db.example.com port=5439 user=x", host: "db.example.com", port: "5439", ok: true},
		{dsn: "garbage", ok: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.dsn, func(t *testing.T) {
			t.Parallel()
			h, p, ok := hostPortFromPostgresDSN(tc.dsn)
			if ok != tc.ok {
				t.Fatalf("ok=%v want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if h != tc.host || p != tc.port {
				t.Fatalf("got %s:%s, want %s:%s", h, p, tc.host, tc.port)
			}
		})
	}
}

func TestTenantCreateRequiresArgs(t *testing.T) {
	t.Parallel()
	deps, _, stderr := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "tenant", "create"}, deps)
	if exit != 2 {
		t.Fatalf("expected exit 2 for missing args, got %d", exit)
	}
	if !strings.Contains(stderr.String(), "--slug is required") {
		t.Fatalf("expected slug error, got %q", stderr.String())
	}
}

func TestTenantCreateRequiresAdminToken(t *testing.T) {
	t.Parallel()
	deps, _, stderr := newTestDeps(map[string]string{})
	exit := runApp(context.Background(), []string{"ec-cli", "tenant", "create",
		"--slug=tenant-x", "--name=Tenant X", "--email=owner@example.com"}, deps)
	if exit != 2 {
		t.Fatalf("expected exit 2 without token, got %d", exit)
	}
	if !strings.Contains(stderr.String(), "EC_ADMIN_TOKEN") {
		t.Fatalf("expected EC_ADMIN_TOKEN message, got %q", stderr.String())
	}
}

func TestTenantCreateHTTPHappyPath(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer admin-token-xyz" {
			t.Errorf("expected bearer token header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(tenantCreateResponse{
			TenantID:  "tenant-uuid-1",
			Slug:      "tenant-x",
			Name:      "Tenant X",
			Plan:      "starter",
			Status:    "provisioning",
			CreatedAt: "2026-05-09T00:00:00Z",
		})
	}))
	defer server.Close()
	deps, stdout, stderr := newTestDeps(map[string]string{
		"EC_API_URL":     server.URL,
		"EC_ADMIN_TOKEN": "admin-token-xyz",
	})
	exit := runApp(context.Background(), []string{"ec-cli", "tenant", "create",
		"--slug=tenant-x", "--name=Tenant X", "--email=owner@example.com", "--json", "--timeout-seconds=2"}, deps)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", exit, stderr.String())
	}
	var resp tenantCreateResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON output, got %q (%v)", stdout.String(), err)
	}
	if resp.TenantID != "tenant-uuid-1" {
		t.Fatalf("tenant id = %q, want tenant-uuid-1", resp.TenantID)
	}
}

func TestTenantCreateServerErrorPropagates(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"slug_taken"}`))
	}))
	defer server.Close()
	deps, _, stderr := newTestDeps(map[string]string{
		"EC_API_URL":     server.URL,
		"EC_ADMIN_TOKEN": "admin-token",
	})
	exit := runApp(context.Background(), []string{"ec-cli", "tenant", "create",
		"--slug=tenant-x", "--name=Tenant X", "--email=owner@example.com", "--timeout-seconds=2"}, deps)
	if exit != 1 {
		t.Fatalf("expected exit 1 for server error, got %d", exit)
	}
	if !strings.Contains(stderr.String(), "409") {
		t.Fatalf("expected 409 in stderr, got %q", stderr.String())
	}
}

func TestPluginValidateRequiresPath(t *testing.T) {
	t.Parallel()
	deps, _, stderr := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "plugin", "validate"}, deps)
	if exit != 2 {
		t.Fatalf("expected exit 2, got %d", exit)
	}
	if !strings.Contains(stderr.String(), "--path is required") {
		t.Fatalf("expected --path message, got %q", stderr.String())
	}
}

func TestPluginValidateBadDirectory(t *testing.T) {
	t.Parallel()
	deps, _, stderr := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "plugin", "validate", "--path=/no/such/path"}, deps)
	if exit != 1 {
		t.Fatalf("expected exit 1, got %d", exit)
	}
	if !strings.Contains(stderr.String(), "stat") {
		t.Fatalf("expected stat error in stderr, got %q", stderr.String())
	}
}

func TestPluginValidateMissingManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	deps, stdout, _ := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "plugin", "validate", "--path=" + dir}, deps)
	if exit != 1 {
		t.Fatalf("expected exit 1 for missing manifest, got %d", exit)
	}
	if !strings.Contains(stdout.String(), "manifest.json missing") {
		t.Fatalf("expected manifest missing issue, got %q", stdout.String())
	}
}

func TestPluginValidateValidManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := map[string]any{
		"slug":    "hello",
		"name":    "Hello Plugin",
		"version": "1.0.0",
		"vendor":  "Example",
	}
	body, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package hello\n"), 0o644); err != nil {
		t.Fatalf("write hello.go: %v", err)
	}
	deps, stdout, stderr := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "plugin", "validate", "--path=" + dir, "--json"}, deps)
	if exit != 0 {
		t.Fatalf("expected exit 0 for valid manifest, got %d (stderr=%q)", exit, stderr.String())
	}
	var report pluginValidationReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("expected JSON output, got %q (%v)", stdout.String(), err)
	}
	if !report.ManifestOK {
		t.Fatalf("expected manifest_ok=true, got %+v", report)
	}
	if report.Counts["go_files"] != 1 {
		t.Fatalf("expected go_files=1, got %d", report.Counts["go_files"])
	}
	if len(report.Suggestions) == 0 {
		t.Fatalf("expected at least one suggestion (no test files), got 0")
	}
}

func TestPluginValidateInvalidManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"slug":"BAD","name":""}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	deps, stdout, _ := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "plugin", "validate", "--path=" + dir}, deps)
	if exit != 1 {
		t.Fatalf("expected exit 1 for bad manifest, got %d", exit)
	}
	if !strings.Contains(stdout.String(), "manifest validation failed") {
		t.Fatalf("expected manifest validation failure, got %q", stdout.String())
	}
}

func TestNowFromEnvProducesISO8601(t *testing.T) {
	t.Parallel()
	got := nowFromEnv()
	if _, err := time.Parse(time.RFC3339Nano, got); err != nil {
		t.Fatalf("nowFromEnv = %q, not RFC3339Nano: %v", got, err)
	}
}

func TestMainImplWithDefaults(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exit := mainImpl(context.Background(), []string{"ec-cli", "version"}, stdout, stderr, func(string) string { return "" })
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
}

func TestHostPortFromRedisURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		host string
		port string
		ok   bool
	}{
		{name: "with_port", raw: "redis://example.com:6380", host: "example.com", port: "6380", ok: true},
		{name: "default_port", raw: "redis://example.com", host: "example.com", port: "6379", ok: true},
		{name: "with_password", raw: "redis://:pass@db.example.com:6390", host: "db.example.com", port: "6390", ok: true},
		{name: "garbage", raw: "://", ok: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, p, ok := hostPortFromRedisURL(tc.raw)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if h != tc.host || p != tc.port {
				t.Fatalf("got %s:%s, want %s:%s", h, p, tc.host, tc.port)
			}
		})
	}
}

func TestDoctorAPICheckPasses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	deps, _, _ := newTestDeps(map[string]string{"EC_API_URL": server.URL})
	check := checkAPI(context.Background(), deps, time.Second)
	if !check.OK {
		t.Fatalf("expected mc-api OK, got %+v", check)
	}
}

func TestDoctorAPICheckFailsOnNon200(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	deps, _, _ := newTestDeps(map[string]string{"EC_API_URL": server.URL})
	check := checkAPI(context.Background(), deps, time.Second)
	if check.OK {
		t.Fatalf("expected mc-api unhealthy, got %+v", check)
	}
}

func TestDoctorPostgresUpProbe(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	dsn := "postgres://user:pass@" + net.JoinHostPort(host, port) + "/db"
	deps, _, _ := newTestDeps(map[string]string{"ECOMMERCE_DB_URL": dsn})
	check := checkPostgres(context.Background(), deps, time.Second)
	if !check.OK {
		t.Fatalf("expected postgres OK, got %+v", check)
	}
}

func TestDoctorPostgresInvalidDSN(t *testing.T) {
	t.Parallel()
	deps, _, _ := newTestDeps(map[string]string{"ECOMMERCE_DB_URL": "garbage"})
	check := checkPostgres(context.Background(), deps, time.Second)
	if check.OK {
		t.Fatalf("expected postgres invalid, got %+v", check)
	}
}

func TestDoctorRedisUpProbe(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	deps, _, _ := newTestDeps(map[string]string{
		"ECOMMERCE_REDIS_URL": "redis://" + net.JoinHostPort(host, port),
	})
	check := checkRedis(context.Background(), deps, time.Second)
	if !check.OK {
		t.Fatalf("expected redis OK, got %+v", check)
	}
}

func TestDoctorTemporalUpProbe(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	deps, _, _ := newTestDeps(map[string]string{"ECOMMERCE_TEMPORAL_ADDR": listener.Addr().String()})
	check := checkTemporal(context.Background(), deps, time.Second)
	if !check.OK {
		t.Fatalf("expected temporal OK, got %+v", check)
	}
}

func TestDoctorTemporalInvalidAddr(t *testing.T) {
	t.Parallel()
	deps, _, _ := newTestDeps(map[string]string{"ECOMMERCE_TEMPORAL_ADDR": "no-colon"})
	check := checkTemporal(context.Background(), deps, time.Second)
	if check.OK {
		t.Fatalf("expected temporal invalid, got %+v", check)
	}
}

func TestRunPluginUnknownSubcommand(t *testing.T) {
	t.Parallel()
	deps, _, stderr := newTestDeps(nil)
	exit := runPlugin(context.Background(), []string{"vape"}, deps)
	if exit != 2 {
		t.Fatalf("expected exit 2, got %d", exit)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("expected unknown subcommand message")
	}
}

func TestRunTenantUnknownSubcommand(t *testing.T) {
	t.Parallel()
	deps, _, stderr := newTestDeps(nil)
	exit := runTenant(context.Background(), []string{"melt"}, deps)
	if exit != 2 {
		t.Fatalf("expected exit 2, got %d", exit)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("expected unknown subcommand message")
	}
}

func TestRunPluginNoSubcommand(t *testing.T) {
	t.Parallel()
	deps, _, stderr := newTestDeps(nil)
	if exit := runPlugin(context.Background(), nil, deps); exit != 2 {
		t.Fatalf("expected exit 2, got %d", exit)
	}
	if !strings.Contains(stderr.String(), "subcommand required") {
		t.Fatalf("expected subcommand required message")
	}
}

func TestRunTenantNoSubcommand(t *testing.T) {
	t.Parallel()
	deps, _, stderr := newTestDeps(nil)
	if exit := runTenant(context.Background(), nil, deps); exit != 2 {
		t.Fatalf("expected exit 2, got %d", exit)
	}
	if !strings.Contains(stderr.String(), "subcommand required") {
		t.Fatalf("expected subcommand required message")
	}
}

func TestValidateTenantCreateBadEmail(t *testing.T) {
	t.Parallel()
	err := validateTenantCreate(tenantCreateRequest{
		Slug: "tenant-x", Name: "Tenant", Email: "not-an-email",
	})
	if err == nil || !strings.Contains(err.Error(), "email") {
		t.Fatalf("expected email error, got %v", err)
	}
}

func TestPluginValidateBadJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	deps, stdout, _ := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "plugin", "validate", "--path=" + dir}, deps)
	if exit != 1 {
		t.Fatalf("expected exit 1, got %d", exit)
	}
	if !strings.Contains(stdout.String(), "not valid JSON") {
		t.Fatalf("expected not valid JSON, got %q", stdout.String())
	}
}

func TestPluginValidateNotADir(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	file := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	deps, _, stderr := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "plugin", "validate", "--path=" + file}, deps)
	if exit != 1 {
		t.Fatalf("expected exit 1, got %d", exit)
	}
	if !strings.Contains(stderr.String(), "not a directory") {
		t.Fatalf("expected not a directory msg, got %q", stderr.String())
	}
}

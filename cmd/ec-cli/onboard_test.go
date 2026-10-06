package main

import (
	"bytes"
	"encoding/json"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The customer-store onboarding check (v18870-4): the fixture store serves
// the two live endpoints the gate reads; the happy config passes everything,
// and each rule's violation fails exactly its row.
//
// MUTANT: drop the administrator refusal in storeonboard's liveUserRole
// consumer and the admin-role row goes green (undetected privilege).

// roleShape steers the users/me answer: "plain" (one role), "admin",
// "second-admin" ([shop_manager, administrator] — the round-1 fail-open
// hole), "unreadable" (no roles field), "garbage" (non-JSON body).
func fixtureStore(t *testing.T, roleShape string) *httptest.Server {
	t.Helper()
	adminRole := roleShape == "admin"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /wp-json/wc/v3/system_status", func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "ck_fixture" || p != "cs_fixture" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"environment":{"wp_version":"6.5"}}`))
	})
	mux.HandleFunc("GET /wp-json/wp/v2/users/me", func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "agent-bot" || p != "abcd efgh ijkl mnop" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch roleShape {
		case "garbage":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html>not json</html>`))
			return
		case "unreadable":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":7,"name":"agent-bot"}`))
			return
		case "second-admin":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":7,"name":"agent-bot","roles":["shop_manager","administrator"]}`))
			return
		}
		role := "shop_manager"
		if adminRole {
			role = "administrator"
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":7,"name":"agent-bot","roles":["` + role + `"]}`))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func writeOnboardConfig(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	cfg := map[string]any{
		"store_url":         "REPLACED-BY-TEST",
		"key_permissions":   "read_write",
		"agent_user_login":  "agent-bot",
		"agent_user_role":   "shop_manager",
		"data_statement":    map[string]any{"version": "2026-10-a", "signed_at": "2026-10-05T22:00:00Z"},
		"approver":          map[string]any{"name": "Dana Customer", "email": "dana@example.com"},
	}
	if mutate != nil {
		mutate(cfg)
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	path := filepath.Join(t.TempDir(), "store.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func runOnboardCheck(t *testing.T, cfgPath string, env map[string]string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	deps := appDeps{stdout: &stdout, stderr: &stderr, getenv: func(k string) string { return env[k] }}
	code := runOnboard(context.Background(), []string{"check", "--config", cfgPath}, deps)
	return code, stdout.String() + stderr.String()
}

func TestOnboardCheckFixtureStorePasses(t *testing.T) {
	store := fixtureStore(t, "plain")
	path := writeOnboardConfig(t, func(m map[string]any) { m["store_url"] = store.URL })
	code, out := runOnboardCheck(t, path, map[string]string{
		"EC_STORE_KEY": "ck_fixture", "EC_STORE_SECRET": "cs_fixture", "EC_STORE_APP_PASSWORD": "abcd efgh ijkl mnop",
	})
	if code != 0 {
		t.Fatalf("the dry-run onboarding of a fixture store must pass, exit=%d output:\n%s", code, out)
	}
	for _, row := range []string{"PASS record", "PASS store-reachable", "PASS app-password", "ONBOARDING CHECK: 3/3 PASS"} {
		if !strings.Contains(out, row) {
			t.Fatalf("expected %q in output:\n%s", row, out)
		}
	}
}

func TestOnboardCheckEachRuleFailsItsRow(t *testing.T) {
	store := fixtureStore(t, "admin") // the live user IS an administrator
	path := writeOnboardConfig(t, func(m map[string]any) {
		m["store_url"] = store.URL
		m["agent_user_role"] = "administrator" // the record agrees — both must refuse
	})
	code, out := runOnboardCheck(t, path, map[string]string{
		"EC_STORE_KEY": "ck_fixture", "EC_STORE_SECRET": "cs_fixture", "EC_STORE_APP_PASSWORD": "abcd efgh ijkl mnop",
	})
	if code == 0 {
		t.Fatalf("an administrator agent user must fail the gate:\n%s", out)
	}
	if !strings.Contains(out, "FAIL app-password") {
		t.Fatalf("the live administrator refusal must fail the app-password row:\n%s", out)
	}

	// A read-only key cannot publish — the minimum-scope rule.
	path = writeOnboardConfig(t, func(m map[string]any) {
		m["store_url"] = store.URL
		m["key_permissions"] = "read"
	})
	_, out = runOnboardCheck(t, path, map[string]string{})
	if !strings.Contains(out, "key_permissions=read_write") {
		t.Fatalf("a read-only key must name the minimum scope in the failing record row:\n%s", out)
	}

	// An unsigned statement fails the record row.
	path = writeOnboardConfig(t, func(m map[string]any) {
		m["store_url"] = store.URL
		m["data_statement"] = map[string]any{"version": "", "signed_at": ""}
	})
	_, out = runOnboardCheck(t, path, map[string]string{})
	if !strings.Contains(out, "data_statement.version+signed_at") {
		t.Fatalf("an unsigned statement must fail the record row:\n%s", out)
	}

	// Round 1, fail-open hole 1: a user whose roles list cannot be read
	// (hidden context, non-JSON body) must FAIL the row, not pass silently.
	unreadable := fixtureStore(t, "unreadable")
	path = writeOnboardConfig(t, func(m map[string]any) { m["store_url"] = unreadable.URL })
	code, out = runOnboardCheck(t, path, map[string]string{
		"EC_STORE_KEY": "ck_fixture", "EC_STORE_SECRET": "cs_fixture", "EC_STORE_APP_PASSWORD": "abcd efgh ijkl mnop",
	})
	if code == 0 || !strings.Contains(out, "roles are unreadable") {
		t.Fatalf("an unreadable roles list must fail closed:\n%s", out)
	}

	// Round 1, fail-open hole 2: [shop_manager, administrator] must FAIL —
	// any administrator role refuses the gate, not only the first listed.
	secondAdmin := fixtureStore(t, "second-admin")
	path = writeOnboardConfig(t, func(m map[string]any) { m["store_url"] = secondAdmin.URL })
	code, out = runOnboardCheck(t, path, map[string]string{
		"EC_STORE_KEY": "ck_fixture", "EC_STORE_SECRET": "cs_fixture", "EC_STORE_APP_PASSWORD": "abcd efgh ijkl mnop",
	})
	if code == 0 || !strings.Contains(out, "administrator role") {
		t.Fatalf("a second-role administrator must fail the gate:\n%s", out)
	}

	// Wrong credentials fail the live rows, not the record row.
	path = writeOnboardConfig(t, nil)
	_ = path
	goodStore := fixtureStore(t, "plain")
	path = writeOnboardConfig(t, func(m map[string]any) { m["store_url"] = goodStore.URL })
	code, out = runOnboardCheck(t, path, map[string]string{
		"EC_STORE_KEY": "ck_wrong", "EC_STORE_SECRET": "cs_fixture", "EC_STORE_APP_PASSWORD": "abcd efgh ijkl mnop",
	})
	if code == 0 || !strings.Contains(out, "FAIL store-reachable") {
		t.Fatalf("wrong REST credentials must fail the store-reachable row:\n%s", out)
	}
}

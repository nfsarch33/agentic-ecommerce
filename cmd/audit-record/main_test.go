package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// board is a minimal in-memory sprint board API.
type board struct {
	mu   sync.Mutex
	list []map[string]any
}

func (b *board) serve(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer tok" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodPost {
		var c map[string]any
		_ = json.NewDecoder(r.Body).Decode(&c)
		c["created_at"] = time.Now().UTC().Format(time.RFC3339)
		b.list = append(b.list, c)
		w.WriteHeader(http.StatusCreated)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"comments": b.list})
}

func TestStepThenKPIOverTheBoard(t *testing.T) {
	b := &board{}
	srv := httptest.NewServer(http.HandlerFunc(b.serve))
	defer srv.Close()
	getenv := func(k string) string {
		return map[string]string{"EC_BOARD_URL": srv.URL, "EC_BOARD_TOKEN": "tok"}[k]
	}
	ctx := context.Background()
	steps := []string{"research", "price", "publish", "verify", "rollback"}
	for _, s := range steps {
		args := []string{"step", "--ticket", "T", "--job", "j1", "--run", "r1", "--step", s}
		if s == "publish" {
			args = append(args, "--status", "failed", "--failure", "store answered 503")
		}
		var out, errb bytes.Buffer
		if rc := run(ctx, args, getenv, &out, &errb); rc != 0 {
			t.Fatalf("step %s rc=%d stderr=%s", s, rc, errb.String())
		}
	}
	var again bytes.Buffer
	if rc := run(ctx, []string{"step", "--ticket", "T", "--job", "j1", "--run", "r1", "--step", "price"}, getenv, &again, &bytes.Buffer{}); rc != 0 || !strings.HasPrefix(again.String(), "ALREADY RECORDED") {
		t.Fatalf("re-record rc=%d out=%q, want ALREADY RECORDED", rc, again.String())
	}
	if len(b.list) != 5 || b.list[0]["author"] != "listing-job" {
		t.Fatalf("board holds %d notes (author %v), want 5", len(b.list), b.list[0]["author"])
	}
	if body := b.list[2]["body"].(string); !strings.Contains(body, "failure: store answered 503") || !strings.Contains(body, "cost=unknown (no ledger configured)") {
		t.Fatalf("publish body = %q", body)
	}
	var out bytes.Buffer
	if rc := run(ctx, []string{"kpi", "--ticket", "T"}, getenv, &out, &bytes.Buffer{}); rc != 0 {
		t.Fatalf("kpi rc=%d", rc)
	}
	if !strings.Contains(out.String(), "jobs=1 complete=1") || !strings.Contains(out.String(), "job=j1 run=r1 steps=5/5 complete=true failed=publish audit_gaps=publish,rollback") {
		t.Fatalf("kpi out = %s", out.String())
	}
}

func TestUsageAndRefusals(t *testing.T) {
	ctx := context.Background()
	none := func(string) string { return "" }
	if rc := run(ctx, []string{"step"}, none, &bytes.Buffer{}, &bytes.Buffer{}); rc != 2 {
		t.Fatalf("no board env rc=%d, want 2", rc)
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close() // connection refused at once
	env := func(k string) string { return map[string]string{"EC_BOARD_URL": deadURL, "EC_BOARD_TOKEN": "x"}[k] }
	if rc := run(ctx, []string{"step", "--ticket", "T", "--job", "j", "--run", "r", "--step", "publish", "--status", "failed"}, env, &bytes.Buffer{}, &bytes.Buffer{}); rc != 2 {
		t.Fatalf("failed without --failure rc=%d, want 2", rc)
	}
	if rc := run(ctx, []string{"step", "--ticket", "T", "--job", "j", "--run", "r", "--step", "bogus"}, env, &bytes.Buffer{}, &bytes.Buffer{}); rc != 1 {
		t.Fatalf("unknown step rc=%d, want 1 (not recorded)", rc)
	}
	// a board that is down is a refused record, exit 1
	if rc := run(ctx, []string{"step", "--ticket", "T", "--job", "j", "--run", "r", "--step", "price"}, env, &bytes.Buffer{}, &bytes.Buffer{}); rc != 1 {
		t.Fatalf("board down rc=%d, want 1", rc)
	}
	if rc := run(ctx, []string{"kpi"}, env, &bytes.Buffer{}, &bytes.Buffer{}); rc != 2 {
		t.Fatalf("kpi without ticket rc=%d, want 2", rc)
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/evomap"
)

func writeFixture(t *testing.T, path string, caps []evomap.Capsule) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer f.Close()
	for _, c := range caps {
		if err := json.NewEncoder(f).Encode(c); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}
}

func TestMainImplWritesCapsule(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in := filepath.Join(dir, "evomap.ndjson")
	out := filepath.Join(dir, "out.md")
	writeFixture(t, in, []evomap.Capsule{
		{EventAt: time.Now().UTC(), Binary: "mc-api", KPIs: evomap.KPIs{ThroughputRPS: 100, P95Ms: 50}},
		{EventAt: time.Now().UTC(), Binary: "agent-worker", KPIs: evomap.KPIs{ThroughputRPS: 50, P95Ms: 25}},
	})
	getenv := func(k string) string { return "" }
	var stdout, stderr bytes.Buffer
	exit := mainImpl(context.Background(), []string{"evomap-rollup", "-in", in, "-out", out}, &stdout, &stderr, getenv)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "EC Stack Daily Rollup") {
		t.Errorf("missing rollup header: %s", data)
	}
}

func TestMainImplNoCapsules(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in := filepath.Join(dir, "evomap.ndjson") // does not exist
	out := filepath.Join(dir, "out.md")
	getenv := func(k string) string { return "" }
	var stdout, stderr bytes.Buffer
	exit := mainImpl(context.Background(), []string{"evomap-rollup", "-in", in, "-out", out}, &stdout, &stderr, getenv)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("expected no output for empty input, got %v", err)
	}
}

func TestMainImplWhenOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in := filepath.Join(dir, "evomap.ndjson")
	out := filepath.Join(dir, "out.md")
	writeFixture(t, in, []evomap.Capsule{
		{EventAt: time.Now().UTC(), Binary: "mc-api", KPIs: evomap.KPIs{ThroughputRPS: 1}},
	})
	getenv := func(k string) string { return "" }
	var stdout, stderr bytes.Buffer
	exit := mainImpl(context.Background(), []string{
		"evomap-rollup", "-in", in, "-out", out, "-when", "2026-05-09T12:00:00Z",
	}, &stdout, &stderr, getenv)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	data, _ := os.ReadFile(out)
	if !strings.Contains(string(data), "2026-05-09") {
		t.Errorf("missing date in output: %s", data)
	}
}

func TestParseArgsRejectsUnknownFlag(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "" }
	_, err := parseArgs([]string{"evomap-rollup", "--no-such-flag"}, getenv, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetenvOrDefault(t *testing.T) {
	t.Parallel()
	getenv := func(k string) string {
		if k == "X" {
			return "v"
		}
		return ""
	}
	if got := getenvOrDefault(getenv, "X", "fallback"); got != "v" {
		t.Errorf("X=%q, want v", got)
	}
	if got := getenvOrDefault(getenv, "Y", "fallback"); got != "fallback" {
		t.Errorf("Y=%q, want fallback", got)
	}
}

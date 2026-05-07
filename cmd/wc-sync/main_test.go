package main

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
)

func TestRunDryRun(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := run(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)), noopChannel{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !bytes.Contains(buf.Bytes(), []byte("wc-sync.dry_run")) {
		t.Fatalf("log output = %s", buf.String())
	}
}

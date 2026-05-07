package main

import (
	"bytes"
	"log/slog"
	"testing"
)

func TestRunLogsReady(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	run(slog.New(slog.NewJSONHandler(&buf, nil)))

	if !bytes.Contains(buf.Bytes(), []byte("content-worker.ready")) {
		t.Fatalf("log output = %s", buf.String())
	}
}

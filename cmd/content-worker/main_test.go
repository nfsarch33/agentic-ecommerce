package main

import (
	"bytes"
	"io"
	"log/slog"
	"os"
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

// TestMainEmitsReadySignal exercises the unexported entrypoint that wires
// stdout into run; it covers the os.Stdout slog wiring that TestRunLogsReady
// cannot reach. We swap os.Stdout for a pipe so test output stays clean.
func TestMainEmitsReadySignal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	originalStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = r.Close()
	})

	main()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	captured, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	if !bytes.Contains(captured, []byte("content-worker.ready")) {
		t.Fatalf("main stdout = %s", string(captured))
	}
}

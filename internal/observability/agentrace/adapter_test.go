package agentrace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// safeBuffer is a goroutine-safe bytes.Buffer for the writer/reader
// concurrency in the smoke tests.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newAdapterForTest(t *testing.T, sink Sink) *Adapter {
	t.Helper()
	a, err := NewAdapter(nil, Config{
		Sink:           sink,
		BufferSize:     64,
		FlushInterval:  10 * time.Millisecond,
		WriteTimeout:   500 * time.Millisecond,
		TransportLabel: "alias:node-a.agentrace",
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	return a
}

func TestAdapter_RejectsMissingSink(t *testing.T) {
	t.Parallel()
	if _, err := NewAdapter(nil, Config{}); !errors.Is(err, ErrSinkRequired) {
		t.Fatalf("NewAdapter err=%v want ErrSinkRequired", err)
	}
}

func TestAdapter_EmitWritesNDJSON(t *testing.T) {
	t.Parallel()
	sink := &safeBuffer{}
	a := newAdapterForTest(t, sink)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := a.Emit(ctx, Event{
			Type: "tool_call", Tool: "Read", Outcome: "ok", DurationS: 0.1,
		}); err != nil {
			t.Fatalf("Emit[%d]: %v", i, err)
		}
	}
	waitForLines(t, sink, 3)
	scanner := bufio.NewScanner(strings.NewReader(sink.String()))
	count := 0
	for scanner.Scan() {
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("ndjson decode: %v", err)
		}
		if ev.Type != "tool_call" || ev.Tool != "Read" {
			t.Fatalf("event = %+v want type=tool_call tool=Read", ev)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("ndjson rows = %d want 3", count)
	}
	stats := a.Stats()
	if stats.Submitted != 3 || stats.Written != 3 {
		t.Fatalf("stats = %+v want submitted=3 written=3", stats)
	}
}

func TestAdapter_SmokeTenEvents(t *testing.T) {
	t.Parallel()
	sink := &safeBuffer{}
	a := newAdapterForTest(t, sink)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if err := a.Emit(ctx, Event{
			Type: "tool_call", Tool: "Shell", SessionID: "s-smoke",
		}); err != nil {
			t.Fatalf("Emit[%d]: %v", i, err)
		}
	}
	waitForLines(t, sink, 10)
	if got := strings.Count(sink.String(), "\n"); got < 10 {
		t.Fatalf("ndjson lines = %d want >= 10", got)
	}
}

func TestAdapter_RejectsEmitAfterClose(t *testing.T) {
	t.Parallel()
	a := newAdapterForTest(t, &safeBuffer{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := a.Emit(context.Background(), Event{Type: "tool_call"}); !errors.Is(err, ErrAdapterClosed) {
		t.Fatalf("Emit after close err=%v want ErrAdapterClosed", err)
	}
}

func TestAdapter_HonoursContextBudget(t *testing.T) {
	t.Parallel()
	sink := &safeBuffer{}
	a := newAdapterForTest(t, sink)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Emit(ctx, Event{Type: "tool_call"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Emit err=%v want context.Canceled", err)
	}
}

func TestAdapter_FillsTimestampAndDefaultType(t *testing.T) {
	t.Parallel()
	sink := &safeBuffer{}
	frozen := time.Date(2026, 5, 11, 13, 0, 0, 0, time.UTC)
	a, err := NewAdapter(nil, Config{
		Sink:          sink,
		FlushInterval: 5 * time.Millisecond,
		Now:           func() time.Time { return frozen },
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = a.Close(ctx)
	}()
	if err := a.Emit(context.Background(), Event{Tool: "Read"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	waitForLines(t, sink, 1)
	var ev Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(sink.String())), &ev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !ev.Timestamp.Equal(frozen) {
		t.Fatalf("timestamp = %s want %s", ev.Timestamp, frozen)
	}
	if ev.Type != "tool_call" {
		t.Fatalf("type = %q want default tool_call", ev.Type)
	}
}

func TestValidateTransportTarget(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{"empty", "", true},
		{"raw_http", "http://127.0.0.1:8100/api/insights", true},
		{"raw_https", "https://node-a.example.com/agentrace", true},
		{"raw_ipv4", "127.0.0.1:9100", true},
		{"tailscale_ts_net", "node-a.tail-foo.ts.net:9100", true},
		{"tailscale_ipv4", "100.64.0.5:9100", true},
		{"tcp_scheme", "tcp://node-a:9100", true},
		{"alias_ok", "alias:node-a.agentrace", false},
		{"abs_path_ok", "/var/log/agentrace/events.jsonl", false},
		{"rel_path_ok", ".local/agentrace/events.jsonl", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTransportTarget(tc.target)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateTransportTarget(%q) err=%v wantErr=%v", tc.target, err, tc.wantErr)
			}
		})
	}
}

func TestAdapter_RingSaturationReturnsTypedError(t *testing.T) {
	t.Parallel()
	sink := &safeBuffer{}
	a, err := NewAdapter(nil, Config{
		Sink:          sink,
		BufferSize:    2,
		FlushInterval: time.Hour, // never flushes during test; Close drains
		WriteTimeout:  20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = a.Close(ctx)
	}()
	for i := 0; i < 2; i++ {
		if err := a.Emit(context.Background(), Event{Type: "tool_call"}); err != nil {
			t.Fatalf("Emit[%d]: %v", i, err)
		}
	}
	err = a.Emit(context.Background(), Event{Type: "tool_call"})
	if !errors.Is(err, ErrRingSaturated) {
		t.Fatalf("Emit on saturated ring err=%v want ErrRingSaturated", err)
	}
	if a.Stats().Dropped != 1 {
		t.Fatalf("Stats.Dropped = %d want 1", a.Stats().Dropped)
	}
}

// waitForLines polls the sink until it observes at least n NDJSON
// lines, or fails the test after a short deadline. Avoids a sleep
// race in the writer-loop assertions.
func waitForLines(t *testing.T, sink *safeBuffer, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(sink.String(), "\n") >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitForLines: only %d lines after deadline (want %d). Buffer:\n%s",
		strings.Count(sink.String(), "\n"), n, sink.String())
}

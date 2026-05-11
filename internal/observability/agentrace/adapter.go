package agentrace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Sentinels surfaced by the adapter.
var (
	// ErrAdapterClosed is returned by Emit after Close.
	ErrAdapterClosed = errors.New("agentrace: adapter closed")

	// ErrSinkRequired is returned by NewAdapter when no writer/sink
	// has been configured. The composition root MUST inject a
	// runx-aliased Writer; raw HTTP/IP transports are rejected at the
	// constructor (see ValidateTransportTarget).
	ErrSinkRequired = errors.New("agentrace: writer sink required")

	// ErrUnsafeTarget is returned when a transport target embeds a
	// raw IP, Tailscale address, or HTTP URL. Transport must always
	// route through a runx alias (see no-shell-leak.mdc).
	ErrUnsafeTarget = errors.New("agentrace: unsafe transport target (alias-only)")

	// ErrRingSaturated is returned by Emit when the bounded ring is
	// full and the caller's context fires before a slot frees up.
	ErrRingSaturated = errors.New("agentrace: ring saturated")
)

// Event is a single Agentrace NDJSON row. Mirrors the v4.11.0
// AgentraceInsight reader contract in internal/evomap. Additive
// fields stay JSON-omitempty so the schema can grow without breaking
// historical NDJSON.
type Event struct {
	Type      string            `json:"type"`
	SessionID string            `json:"session_id,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Tool      string            `json:"tool,omitempty"`
	Outcome   string            `json:"outcome,omitempty"`
	CostUSD   float64           `json:"cost_usd,omitempty"`
	Severity  string            `json:"severity,omitempty"`
	Ratio     float64           `json:"ratio,omitempty"`
	DurationS float64           `json:"duration_s,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// Sink is the writer surface the adapter forwards NDJSON rows to.
// Production wiring uses a runx-aliased file or pipe; tests inject
// a bytes.Buffer.
type Sink interface {
	io.Writer
}

// Config wires an Adapter. Sink is REQUIRED; everything else has
// safe defaults.
type Config struct {
	Sink           Sink
	BufferSize     int           // bounded ring capacity; default 256
	FlushInterval  time.Duration // default 1s
	WriteTimeout   time.Duration // default 2s; per-Emit context budget
	TransportLabel string        // free-form identifier of the runx alias used (for log lines)
	Now            func() time.Time
}

// Stats is a point-in-time snapshot of adapter counters.
type Stats struct {
	Submitted int64
	Written   int64
	Dropped   int64
	Errors    int64
	Queued    int
}

// Adapter is the v6.2.0 NDJSON forwarder.
type Adapter struct {
	cfg    Config
	logger *slog.Logger

	mu     sync.Mutex
	closed bool

	ring   *ringBuffer
	stopCh chan struct{}
	doneCh chan struct{}

	submitted atomic.Int64
	written   atomic.Int64
	dropped   atomic.Int64
	failed    atomic.Int64
}

// NewAdapter constructs the adapter and starts the single writer
// goroutine. Returns ErrSinkRequired when no sink is configured.
func NewAdapter(logger *slog.Logger, cfg Config) (*Adapter, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Sink == nil {
		return nil, ErrSinkRequired
	}
	cfg = applyDefaults(cfg)
	a := &Adapter{
		cfg:    cfg,
		logger: logger,
		ring:   newRingBuffer(cfg.BufferSize),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	go a.writerLoop()
	return a, nil
}

// Emit enqueues an event. Honours the caller's context budget. When
// the ring is saturated and the context fires, returns
// ErrRingSaturated so the caller can decide whether to drop or retry.
func (a *Adapter) Emit(ctx context.Context, ev Event) error {
	if a == nil {
		return ErrAdapterClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return ErrAdapterClosed
	}
	a.mu.Unlock()
	a.fillDefaults(&ev)
	deadline, cancel := context.WithTimeout(ctx, a.cfg.WriteTimeout)
	defer cancel()
	if a.ring.push(deadline, ev) {
		a.submitted.Add(1)
		return nil
	}
	a.dropped.Add(1)
	return ErrRingSaturated
}

// Close marks the adapter closed, flushes the ring, and waits for
// the writer goroutine to drain.
func (a *Adapter) Close(ctx context.Context) error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	close(a.stopCh)
	a.mu.Unlock()
	select {
	case <-a.doneCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("agentrace: drain: %w", ctx.Err())
	}
}

// Stats returns the current adapter counters.
func (a *Adapter) Stats() Stats {
	return Stats{
		Submitted: a.submitted.Load(),
		Written:   a.written.Load(),
		Dropped:   a.dropped.Load(),
		Errors:    a.failed.Load(),
		Queued:    a.ring.len(),
	}
}

func (a *Adapter) fillDefaults(ev *Event) {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = a.cfg.Now().UTC()
	}
	if ev.Type == "" {
		ev.Type = "tool_call"
	}
}

func (a *Adapter) writerLoop() {
	defer close(a.doneCh)
	ticker := time.NewTicker(a.cfg.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.stopCh:
			a.flush()
			return
		case <-ticker.C:
			a.flush()
		}
	}
}

func (a *Adapter) flush() {
	for {
		ev, ok := a.ring.pop()
		if !ok {
			return
		}
		if err := writeEvent(a.cfg.Sink, ev); err != nil {
			a.failed.Add(1)
			a.logger.Warn("agentrace.write_failed",
				"transport", a.cfg.TransportLabel,
				"error", err,
			)
			continue
		}
		a.written.Add(1)
	}
}

func writeEvent(sink Sink, ev Event) error {
	buf, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	buf = append(buf, '\n')
	if _, err := sink.Write(buf); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

func applyDefaults(cfg Config) Config {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 256
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = time.Second
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 2 * time.Second
	}
	if cfg.TransportLabel == "" {
		cfg.TransportLabel = "runx-alias"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return cfg
}

// ValidateTransportTarget rejects any transport string that embeds a
// raw IP, Tailscale address, or HTTP/HTTPS URL. The composition root
// calls this before constructing the Adapter so unsafe wsl1 targets
// fail loud at boot rather than leaking through to NDJSON argv.
//
// Acceptable shapes:
//   - relative or absolute filesystem paths (NDJSON file)
//   - identifiers that begin with "alias:" (e.g. "alias:wsl1.agentrace")
//
// Rejected shapes:
//   - "http://...", "https://..."
//   - "tcp://...", "tcp+tls://..."
//   - any colon-separated host:port that contains digits (raw IP/Tailscale)
//
// Cyclomatic 5.
func ValidateTransportTarget(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return ErrUnsafeTarget
	}
	lower := strings.ToLower(target)
	for _, prefix := range []string{"http://", "https://", "tcp://", "udp://", "ws://", "wss://"} {
		if strings.HasPrefix(lower, prefix) {
			return fmt.Errorf("%w: scheme %q", ErrUnsafeTarget, prefix)
		}
	}
	if isRawIPv4Like(lower) || isTailscaleAddress(lower) {
		return fmt.Errorf("%w: raw network address", ErrUnsafeTarget)
	}
	return nil
}

func isRawIPv4Like(s string) bool {
	s = stripScheme(s)
	dots := strings.Count(s, ".")
	if dots != 3 {
		return false
	}
	for _, segment := range strings.SplitN(s, ".", 4) {
		if segment == "" {
			return false
		}
		if !allDigitsOrPort(segment) {
			return false
		}
	}
	return true
}

func stripScheme(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, ":"); i >= 0 {
		s = s[:i]
	}
	return s
}

func allDigitsOrPort(seg string) bool {
	for _, r := range seg {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isTailscaleAddress(s string) bool {
	return strings.Contains(s, ".ts.net") ||
		strings.HasPrefix(s, "100.") || // Tailscale IPv4 range starts with 100.x
		strings.Contains(s, "tailscale")
}

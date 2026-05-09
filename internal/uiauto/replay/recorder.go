// Package replay ships the v3.7.0 EC-10-5 replay test harness.
// Records browser-driven uiauto sessions to YAML cassettes (forward-
// compatible with the gopkg.in/dnaeon/go-vcr.v3 schema we already
// use for the v3.1.1 China sourcing cassettes) and replays them
// deterministically for hermetic CI tests.
//
// Recorder mode: wraps the omniparser-bridge HTTP client; intercepts
// request + response pairs (HTTPInteraction), DOM mutations
// (DOMSnapshot), and click/type/scroll events (UIEvent) with
// timestamps. Persists to tests/uiauto/cassettes/<test_name>.yaml.
//
// Player mode: replays the recorded interactions in order; on
// mismatch (recorded request != replayed request) returns
// ErrPlaybackMismatch.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4): Recorder.Capture splits into captureHTTP + captureDOM +
// captureEvent helpers; Player.Next splits into matchRequest +
// dispenseResponse helpers; per-function cyclomatic stays under 6.
package replay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Typed sentinels.
var (
	// ErrCassetteNotFound is returned when the cassette path does
	// not resolve to an existing file.
	ErrCassetteNotFound = errors.New("replay: cassette not found")

	// ErrCassetteCorrupted is returned when the YAML file fails
	// to parse or schema-validate.
	ErrCassetteCorrupted = errors.New("replay: cassette corrupted")

	// ErrPlaybackMismatch is returned by the Player when the
	// caller's request does not match the recorded request at the
	// current cursor position.
	ErrPlaybackMismatch = errors.New("replay: playback mismatch")

	// ErrPlaybackExhausted is returned when the cassette has been
	// fully replayed.
	ErrPlaybackExhausted = errors.New("replay: cassette exhausted")
)

// HTTPInteraction is one recorded request/response pair.
type HTTPInteraction struct {
	Method       string            `yaml:"method"`
	URL          string            `yaml:"url"`
	RequestBody  string            `yaml:"request_body,omitempty"`
	Status       int               `yaml:"status"`
	Headers      map[string]string `yaml:"headers,omitempty"`
	ResponseBody string            `yaml:"response_body,omitempty"`
	OccurredAt   time.Time         `yaml:"occurred_at"`
}

// DOMSnapshot captures a DOM mutation.
type DOMSnapshot struct {
	StepLabel  string    `yaml:"step_label"`
	Snapshot   string    `yaml:"snapshot"`
	OccurredAt time.Time `yaml:"occurred_at"`
}

// UIEvent captures one click/type/scroll/etc.
type UIEvent struct {
	Kind       string    `yaml:"kind"` // click | type | scroll | hover
	Selector   string    `yaml:"selector,omitempty"`
	Value      string    `yaml:"value,omitempty"`
	OccurredAt time.Time `yaml:"occurred_at"`
}

// Cassette is the on-disk envelope. Schema version is part of the
// envelope so future migrations can detect compatibility.
type Cassette struct {
	Version      int               `yaml:"version"`
	Name         string            `yaml:"name"`
	Description  string            `yaml:"description,omitempty"`
	HTTP         []HTTPInteraction `yaml:"http,omitempty"`
	DOMSnapshots []DOMSnapshot     `yaml:"dom_snapshots,omitempty"`
	UIEvents     []UIEvent         `yaml:"ui_events,omitempty"`
}

// CassetteVersion is the canonical schema version. Bump when a
// breaking change happens.
const CassetteVersion = 1

// Recorder wraps an upstream uiauto client and records all
// interactions for later replay. Thread-safe.
type Recorder struct {
	mu       sync.Mutex
	cassette Cassette
	logger   *slog.Logger
	now      func() time.Time
}

// NewRecorder constructs a recorder.
func NewRecorder(name string) *Recorder {
	return &Recorder{
		cassette: Cassette{Version: CassetteVersion, Name: name},
		logger:   slog.Default(),
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// SetClock overrides the clock for tests.
func (r *Recorder) SetClock(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = now
}

// Capture is the front-door for the test harness. The caller hands
// back the upstream RoundTrip result; the recorder stamps a copy.
func (r *Recorder) Capture(req *http.Request, body []byte, resp *http.Response, respBody []byte) {
	if req == nil || resp == nil {
		return
	}
	r.captureHTTP(req, body, resp, respBody)
}

// CaptureDOM stamps a DOM snapshot.
func (r *Recorder) CaptureDOM(stepLabel, snapshot string) {
	r.captureDOM(stepLabel, snapshot)
}

// CaptureEvent stamps a UI event.
func (r *Recorder) CaptureEvent(kind, selector, value string) {
	r.captureEvent(kind, selector, value)
}

func (r *Recorder) captureHTTP(req *http.Request, body []byte, resp *http.Response, respBody []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	headers := map[string]string{}
	for k, vv := range resp.Header {
		if len(vv) == 0 {
			continue
		}
		headers[k] = strings.Join(vv, ",")
	}
	r.cassette.HTTP = append(r.cassette.HTTP, HTTPInteraction{
		Method:       req.Method,
		URL:          req.URL.String(),
		RequestBody:  string(body),
		Status:       resp.StatusCode,
		Headers:      headers,
		ResponseBody: string(respBody),
		OccurredAt:   r.now(),
	})
}

func (r *Recorder) captureDOM(stepLabel, snapshot string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cassette.DOMSnapshots = append(r.cassette.DOMSnapshots, DOMSnapshot{
		StepLabel:  stepLabel,
		Snapshot:   snapshot,
		OccurredAt: r.now(),
	})
}

func (r *Recorder) captureEvent(kind, selector, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cassette.UIEvents = append(r.cassette.UIEvents, UIEvent{
		Kind:       kind,
		Selector:   selector,
		Value:      value,
		OccurredAt: r.now(),
	})
}

// Cassette returns a snapshot copy of the current cassette state.
func (r *Recorder) Cassette() Cassette {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.cassette
	out.HTTP = append([]HTTPInteraction(nil), r.cassette.HTTP...)
	out.DOMSnapshots = append([]DOMSnapshot(nil), r.cassette.DOMSnapshots...)
	out.UIEvents = append([]UIEvent(nil), r.cassette.UIEvents...)
	return out
}

// Save serialises the cassette to disk in YAML form. Parent
// directory is created with 0o755 if missing.
func (r *Recorder) Save(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("replay: mkdir: %w", err)
	}
	out, err := yaml.Marshal(r.cassette)
	if err != nil {
		return fmt.Errorf("replay: marshal: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("replay: write: %w", err)
	}
	return nil
}

// LoadCassette reads + validates a YAML cassette.
func LoadCassette(path string) (Cassette, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Cassette{}, fmt.Errorf("%w: %s", ErrCassetteNotFound, path)
		}
		return Cassette{}, fmt.Errorf("replay: read: %w", err)
	}
	var c Cassette
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Cassette{}, fmt.Errorf("%w: %v", ErrCassetteCorrupted, err)
	}
	if c.Version == 0 {
		return Cassette{}, fmt.Errorf("%w: missing version", ErrCassetteCorrupted)
	}
	if c.Version > CassetteVersion {
		return Cassette{}, fmt.Errorf("%w: cassette version %d > supported %d", ErrCassetteCorrupted, c.Version, CassetteVersion)
	}
	return c, nil
}

// Player replays a previously-recorded cassette.
type Player struct {
	cassette Cassette
	cursor   int
	mu       sync.Mutex
	logger   *slog.Logger
}

// NewPlayer constructs a Player from a Cassette.
func NewPlayer(c Cassette) *Player {
	return &Player{cassette: c, logger: slog.Default()}
}

// LoadPlayer is a convenience for tests.
func LoadPlayer(path string) (*Player, error) {
	c, err := LoadCassette(path)
	if err != nil {
		return nil, err
	}
	return NewPlayer(c), nil
}

// Cassette returns the underlying cassette.
func (p *Player) Cassette() Cassette { return p.cassette }

// Cursor returns the current playback index.
func (p *Player) Cursor() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cursor
}

// Next replays the next HTTP interaction. Decomposes into
// matchRequest + dispenseResponse helpers.
func (p *Player) Next(ctx context.Context, req *http.Request) (*http.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expected, err := p.matchRequest(req)
	if err != nil {
		return nil, err
	}
	return p.dispenseResponse(req, expected), nil
}

// matchRequest returns the expected HTTP interaction at the cursor
// or routes to the mismatch / exhausted branches.
func (p *Player) matchRequest(req *http.Request) (HTTPInteraction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cursor >= len(p.cassette.HTTP) {
		return HTTPInteraction{}, fmt.Errorf("%w: cursor=%d", ErrPlaybackExhausted, p.cursor)
	}
	expected := p.cassette.HTTP[p.cursor]
	gotURL := req.URL.String()
	if expected.Method != req.Method || expected.URL != gotURL {
		return HTTPInteraction{}, fmt.Errorf("%w: cursor=%d expected=%s %s got=%s %s",
			ErrPlaybackMismatch, p.cursor, expected.Method, expected.URL, req.Method, gotURL)
	}
	p.cursor++
	return expected, nil
}

// dispenseResponse fabricates an http.Response from the recorded
// interaction.
func (p *Player) dispenseResponse(req *http.Request, expected HTTPInteraction) *http.Response {
	header := http.Header{}
	for k, v := range expected.Headers {
		header.Set(k, v)
	}
	body := io.NopCloser(strings.NewReader(expected.ResponseBody))
	return &http.Response{
		Status:     fmt.Sprintf("%d %s", expected.Status, http.StatusText(expected.Status)),
		StatusCode: expected.Status,
		Header:     header,
		Body:       body,
		Request:    req,
	}
}

// PlayerTransport adapts a Player to http.RoundTripper. Tests inject
// this into an http.Client to drive uiauto code paths from a
// cassette.
type PlayerTransport struct {
	Player *Player
}

// RoundTrip executes the next recorded interaction.
func (t *PlayerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Player == nil {
		return nil, errors.New("replay: nil player")
	}
	return t.Player.Next(req.Context(), req)
}

// Reset rewinds the player to the start.
func (p *Player) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cursor = 0
}

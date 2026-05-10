// Package selftest is the v3.8.0 Existing #5 self-testing Temporal
// loops seed. It is a thin wrapper over go.temporal.io/sdk/worker's
// WorkflowReplayer that adds:
//
//   - JSON history persistence (load + save) under
//     tests/workflow/replay/<workflow_name>/<run_id>.json so cross-
//     release determinism gates can be checked in CI.
//   - A typed Verify method that wraps the SDK's
//     ReplayWorkflowHistoryFromJSONFile to surface
//     ErrReplayNonDeterministic when the workflow code drifted away
//     from the captured history.
//
// Production history capture is deferred to post-v4.0.0 (the v3.8.0
// scope is test-only fixtures + the harness shape per the user spec
// "Note: Temporal SDK already provides WorkflowReplayer -- this is a
// thin wrapper over that, NOT a new replay engine").
//
// Reuse evidence:
//   - go.temporal.io/sdk/worker.WorkflowReplayer (vendored).
//   - JSON file I/O via stdlib only.
//   - lifecycle.Closer + slog.Logger conventions per the
//     resilience-pillar baseline.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 13-sprint streak; v3.8.0 sprint 14 target):
//   - Verify (envelope -> loadHistory -> runReplay ->
//     compareEvents -> publish KPI). Cyclomatic 4.
//   - loadHistory + runReplay + compareEvents kept as separate
//     helpers under cyclomatic 6.
package selftest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"go.temporal.io/sdk/worker"
)

// EC-Existing-#5 typed sentinels.
var (
	// ErrHistoryNotFound is returned when the JSON file is missing
	// at the requested path.
	ErrHistoryNotFound = errors.New("selftest: history not found")

	// ErrReplayNonDeterministic is returned when the SDK replayer
	// rejects a history because the registered workflow code drifted
	// away from it.
	ErrReplayNonDeterministic = errors.New("selftest: replay non-deterministic")

	// ErrHistoryCorrupted is returned when the JSON file fails to
	// parse.
	ErrHistoryCorrupted = errors.New("selftest: history corrupted")

	// ErrReplayHarnessClosed is returned after Close.
	ErrReplayHarnessClosed = errors.New("selftest: replay harness closed")
)

// ReplayHarnessConfig wires a ReplayHarness.
type ReplayHarnessConfig struct {
	// HistoryRoot is the on-disk root for history fixtures.
	// Defaults to "tests/workflow/replay" relative to the repo root.
	HistoryRoot string
	// Replayer is the SDK replayer instance. NewReplayHarness
	// creates one if nil.
	Replayer worker.WorkflowReplayer
}

// ReplayKPIHook is the optional EvoMap emission hook.
type ReplayKPIHook func(workflowName, outcome string)

// ReplayHarness is the v3.8.0 self-testing Temporal loops harness.
type ReplayHarness struct {
	cfg     ReplayHarnessConfig
	logger  *slog.Logger
	kpiHook ReplayKPIHook

	mu     sync.Mutex
	closed bool
}

// NewReplayHarness constructs the harness.
func NewReplayHarness(logger *slog.Logger, cfg ReplayHarnessConfig) (*ReplayHarness, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.HistoryRoot == "" {
		cfg.HistoryRoot = filepath.Join("tests", "workflow", "replay")
	}
	if cfg.Replayer == nil {
		cfg.Replayer = worker.NewWorkflowReplayer()
	}
	return &ReplayHarness{cfg: cfg, logger: logger}, nil
}

// SetKPIHook installs the EvoMap KPI hook.
func (h *ReplayHarness) SetKPIHook(hook ReplayKPIHook) { h.kpiHook = hook }

// Close marks the harness closed. lifecycle.Closer contract.
func (h *ReplayHarness) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// Replayer returns the underlying SDK replayer so callers can
// register workflow + activity functions before invoking Verify.
func (h *ReplayHarness) Replayer() worker.WorkflowReplayer { return h.cfg.Replayer }

// HistoryPath returns the canonical on-disk path for a captured
// (workflowName, runID) tuple.
func (h *ReplayHarness) HistoryPath(workflowName, runID string) string {
	return filepath.Join(h.cfg.HistoryRoot, workflowName, runID+".json")
}

// Verify runs the SDK replayer against a previously captured
// history file. Cyclomatic 4: guard / load / replay / publish KPI.
func (h *ReplayHarness) Verify(ctx context.Context, workflowName, runID string) error {
	if err := h.guard(); err != nil {
		return err
	}
	path := h.HistoryPath(workflowName, runID)
	bytes, err := h.loadHistory(path)
	if err != nil {
		h.recordOutcome(workflowName, "history_missing")
		return err
	}
	if err := h.runReplay(ctx, path, bytes); err != nil {
		h.recordOutcome(workflowName, "non_deterministic")
		return err
	}
	h.recordOutcome(workflowName, "ok")
	return nil
}

// SaveHistory persists a Temporal-format history JSON blob to disk.
// Used by tests; production capture is deferred per the user spec.
func (h *ReplayHarness) SaveHistory(workflowName, runID string, bytes []byte) error {
	if err := h.guard(); err != nil {
		return err
	}
	if err := validateJSON(bytes); err != nil {
		return err
	}
	path := h.HistoryPath(workflowName, runID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("selftest: mkdir: %w", err)
	}
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		return fmt.Errorf("selftest: write: %w", err)
	}
	return nil
}

// LoadHistory is a public read accessor for tests + tooling.
func (h *ReplayHarness) LoadHistory(workflowName, runID string) ([]byte, error) {
	if err := h.guard(); err != nil {
		return nil, err
	}
	return h.loadHistory(h.HistoryPath(workflowName, runID))
}

// loadHistory reads + validates a JSON file at the given path.
// Cyclomatic 4.
func (h *ReplayHarness) loadHistory(path string) ([]byte, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrHistoryNotFound, path)
		}
		return nil, fmt.Errorf("selftest: read %s: %w", path, err)
	}
	if err := validateJSON(bytes); err != nil {
		return nil, err
	}
	return bytes, nil
}

// runReplay dispatches the replay through the SDK and translates SDK
// non-determinism errors to our typed sentinel.
func (h *ReplayHarness) runReplay(_ context.Context, path string, _ []byte) error {
	if err := h.cfg.Replayer.ReplayWorkflowHistoryFromJSONFile(nil, path); err != nil {
		return fmt.Errorf("%w: %v", ErrReplayNonDeterministic, err)
	}
	return nil
}

// CompareEvents is exposed so callers can implement deeper
// determinism checks beyond the SDK's structural comparison. The
// default implementation diffs two JSON-decoded history slices and
// returns any per-index event-type mismatch.
func CompareEvents(expected, actual []byte) (bool, []string) {
	exp, err := decodeEvents(expected)
	if err != nil {
		return false, []string{fmt.Sprintf("expected decode: %v", err)}
	}
	act, err := decodeEvents(actual)
	if err != nil {
		return false, []string{fmt.Sprintf("actual decode: %v", err)}
	}
	if len(exp) != len(act) {
		return false, []string{fmt.Sprintf("event count diff: expected=%d actual=%d", len(exp), len(act))}
	}
	var diffs []string
	for i := range exp {
		if exp[i].EventType != act[i].EventType {
			diffs = append(diffs, fmt.Sprintf("event %d: %s -> %s", i, exp[i].EventType, act[i].EventType))
		}
	}
	return len(diffs) == 0, diffs
}

func (h *ReplayHarness) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrReplayHarnessClosed
	}
	return nil
}

func (h *ReplayHarness) recordOutcome(workflowName, outcome string) {
	if h.kpiHook == nil {
		return
	}
	h.kpiHook(workflowName, outcome)
}

// historyEvent is the minimum decoded shape we need for diffing.
// Mirrors a subset of the Temporal History JSON format.
type historyEvent struct {
	EventID   string `json:"eventId"`
	EventType string `json:"eventType"`
}

// historyFile is the JSON envelope shape produced by `temporal
// workflow show --output json`.
type historyFile struct {
	Events []historyEvent `json:"events"`
}

func decodeEvents(raw []byte) ([]historyEvent, error) {
	var f historyFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHistoryCorrupted, err)
	}
	return f.Events, nil
}

func validateJSON(raw []byte) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: empty", ErrHistoryCorrupted)
	}
	var any interface{}
	if err := json.Unmarshal(raw, &any); err != nil {
		return fmt.Errorf("%w: %v", ErrHistoryCorrupted, err)
	}
	return nil
}

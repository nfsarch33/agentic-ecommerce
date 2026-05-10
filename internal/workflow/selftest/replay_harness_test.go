package selftest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// minimalHistoryJSON is a synthetic but structurally-valid Temporal
// History JSON shape used for round-trip tests. It is intentionally
// minimal so the test does not depend on a vendored SDK dump.
const minimalHistoryJSON = `{
  "events": [
    {"eventId": "1", "eventType": "WorkflowExecutionStarted"},
    {"eventId": "2", "eventType": "WorkflowTaskScheduled"},
    {"eventId": "3", "eventType": "WorkflowTaskCompleted"},
    {"eventId": "4", "eventType": "WorkflowExecutionCompleted"}
  ]
}`

const driftedHistoryJSON = `{
  "events": [
    {"eventId": "1", "eventType": "WorkflowExecutionStarted"},
    {"eventId": "2", "eventType": "WorkflowTaskScheduled"},
    {"eventId": "3", "eventType": "ActivityTaskScheduled"},
    {"eventId": "4", "eventType": "WorkflowExecutionCompleted"}
  ]
}`

func newTempHarness(t *testing.T) (*ReplayHarness, string) {
	dir := t.TempDir()
	h, err := NewReplayHarness(nil, ReplayHarnessConfig{HistoryRoot: dir})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	return h, dir
}

func TestReplayHarness_CapturesProductionHistory(t *testing.T) {
	t.Parallel()
	h, dir := newTempHarness(t)

	require.NoError(t, h.SaveHistory("order_aggregator", "run-001", []byte(minimalHistoryJSON)))
	loaded, err := h.LoadHistory("order_aggregator", "run-001")
	require.NoError(t, err)
	require.Equal(t, []byte(minimalHistoryJSON), loaded)

	expected := filepath.Join(dir, "order_aggregator", "run-001.json")
	_, statErr := os.Stat(expected)
	require.NoError(t, statErr, "history file written to canonical path")
}

func TestReplayHarness_PersistsToJSON(t *testing.T) {
	t.Parallel()
	h, dir := newTempHarness(t)

	require.NoError(t, h.SaveHistory("dropship_saga", "run-002", []byte(minimalHistoryJSON)))
	require.NoError(t, h.SaveHistory("returns_saga", "run-003", []byte(minimalHistoryJSON)))

	for _, p := range []string{
		filepath.Join(dir, "dropship_saga", "run-002.json"),
		filepath.Join(dir, "returns_saga", "run-003.json"),
	} {
		raw, err := os.ReadFile(p)
		require.NoError(t, err)
		require.Equal(t, minimalHistoryJSON, string(raw), "JSON round-trips byte-for-byte at %s", p)
	}
}

func TestReplayHarness_RejectsCorruptedJSON(t *testing.T) {
	t.Parallel()
	h, _ := newTempHarness(t)

	err := h.SaveHistory("order_aggregator", "bad", []byte("not-json{{{"))
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrHistoryCorrupted))

	err = h.SaveHistory("order_aggregator", "empty", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrHistoryCorrupted))
}

func TestReplayHarness_DetectsNonDeterminism(t *testing.T) {
	t.Parallel()
	ok, diffs := CompareEvents([]byte(minimalHistoryJSON), []byte(driftedHistoryJSON))
	require.False(t, ok)
	require.Len(t, diffs, 1)
	require.Contains(t, diffs[0], "event 2")
}

func TestReplayHarness_ReplayMatchesProductionExpected(t *testing.T) {
	t.Parallel()
	ok, diffs := CompareEvents([]byte(minimalHistoryJSON), []byte(minimalHistoryJSON))
	require.True(t, ok)
	require.Empty(t, diffs)
}

func TestReplayHarness_LoadMissingHistory(t *testing.T) {
	t.Parallel()
	h, _ := newTempHarness(t)
	_, err := h.LoadHistory("does-not-exist", "x")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrHistoryNotFound))
}

func TestReplayHarness_VerifyMissingHistory(t *testing.T) {
	t.Parallel()
	h, _ := newTempHarness(t)
	err := h.Verify(context.Background(), "does-not-exist", "x")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrHistoryNotFound))
}

func TestReplayHarness_ClosedReturnsError(t *testing.T) {
	t.Parallel()
	h, _ := newTempHarness(t)
	require.NoError(t, h.Close(context.Background()))

	_, err := h.LoadHistory("x", "y")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrReplayHarnessClosed))
}

func TestReplayHarness_KPIHookFires(t *testing.T) {
	t.Parallel()
	h, _ := newTempHarness(t)
	var captured []string
	h.SetKPIHook(func(workflowName, outcome string) {
		captured = append(captured, workflowName+":"+outcome)
	})

	_ = h.Verify(context.Background(), "order_aggregator", "missing")
	require.Equal(t, []string{"order_aggregator:history_missing"}, captured)
}

package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestParseModeDefaultsToLegacy(t *testing.T) {
	t.Parallel()

	mode, err := ParseMode("")
	if err != nil {
		t.Fatalf("ParseMode returned error: %v", err)
	}
	if mode != ModeLegacy {
		t.Fatalf("mode = %q, want legacy", mode)
	}
}

func TestParseModeRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	_, err := ParseMode("surprise")
	if err == nil {
		t.Fatal("ParseMode returned nil error for invalid mode")
	}
	if !strings.Contains(err.Error(), "unsupported agent runtime mode") {
		t.Fatalf("ParseMode error = %q, want unsupported mode error", err)
	}
}

func TestRunOnceExecutesBootstrapPathForShadowMode(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	summary, err := RunOnce(
		context.Background(),
		slog.New(slog.NewJSONHandler(&buf, nil)),
		Config{
			Mode:                      ModeShadow,
			ScheduleMaxConcurrentRuns: 2,
		},
	)
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if summary.Submitted != 1 || summary.Succeeded != 1 || summary.Failed != 0 {
		t.Fatalf("summary = %#v, want submitted=1 succeeded=1 failed=0", summary)
	}
	logs := buf.String()
	if !strings.Contains(logs, "agent-runtime.mode_selected") {
		t.Fatalf("logs missing mode_selected entry:\n%s", logs)
	}
	if !strings.Contains(logs, `"mode":"shadow"`) {
		t.Fatalf("logs missing shadow runtime mode:\n%s", logs)
	}
	if !strings.Contains(logs, "agent-worker.scheduler_run_succeeded") {
		t.Fatalf("logs missing scheduler success entry:\n%s", logs)
	}
}

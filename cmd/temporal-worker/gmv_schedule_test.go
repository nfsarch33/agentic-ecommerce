package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
	enumspb "go.temporal.io/api/enums/v1"
)

func TestScheduleAlreadyExists_DetectsCanonicalShapes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"already_exists_camel", errors.New("schedule AlreadyExists"), true},
		{"already_exists_lower", errors.New("schedule already exists"), true},
		{"unrelated", errors.New("connection refused"), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := scheduleAlreadyExists(tc.err); got != tc.want {
				t.Fatalf("scheduleAlreadyExists(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestScheduleOverlapPolicy_MapsStringsToEnum(t *testing.T) {
	t.Parallel()
	if got := scheduleOverlapPolicy(ecworkflow.GMVDailyRefreshOverlapSkip); got != enumspb.SCHEDULE_OVERLAP_POLICY_SKIP {
		t.Fatalf("skip mapping wrong: %v", got)
	}
	if got := scheduleOverlapPolicy("unknown-policy"); got != enumspb.SCHEDULE_OVERLAP_POLICY_SKIP {
		t.Fatalf("unknown should default to skip: %v", got)
	}
}

// TestEnsureGMVDailyRefreshSchedule_NilClient covers the
// defensive no-op branch when the worker is started without a
// reachable Temporal frontend (the warn-and-skip path).
func TestEnsureGMVDailyRefreshSchedule_NilClient(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	// nil client must short-circuit cleanly without panicking.
	ensureGMVDailyRefreshSchedule(context.Background(), logger, nil, "ec-workflows")
}

// TestNewGMVDailyRefreshActivitiesFromEnv_NoDSN covers the
// "ECOMMERCE_DB_URL not set" warn-and-fallback branch which
// should return a non-nil activity surface with an unwired
// executor (workflow returns clean error rather than panicking).
func TestNewGMVDailyRefreshActivitiesFromEnv_NoDSN(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_URL", "")
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	a := newGMVDailyRefreshActivitiesFromEnv(context.Background(), logger)
	if a == nil {
		t.Fatal("activities surface should be non-nil even without DSN")
	}
}

// TestNewGMVDailyRefreshActivitiesFromEnv_BadDSN covers the
// pool-config parse-error branch.
func TestNewGMVDailyRefreshActivitiesFromEnv_BadDSN(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_URL", "://not-a-valid-dsn")
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	a := newGMVDailyRefreshActivitiesFromEnv(context.Background(), logger)
	if a == nil {
		t.Fatal("activities surface should be non-nil even with bad DSN")
	}
}

// TestEnsureGMVDailyRefreshSchedule_NilClientNoLogger covers the
// degenerate path where logger is also nil (shouldn't panic).
func TestEnsureGMVDailyRefreshSchedule_NilClientNoLogger(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("must not panic with nil logger: %v", r)
		}
	}()
	ensureGMVDailyRefreshSchedule(context.Background(), nil, nil, "")
}

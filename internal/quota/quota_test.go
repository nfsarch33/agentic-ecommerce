package quota

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPolicyLimit(t *testing.T) {
	t.Parallel()
	p := PolicyFromLimits(60, 200, 1024, 5)
	cases := []struct {
		metric Metric
		want   int64
	}{
		{MetricAPIPerMinute, 60},
		{MetricAgentRunsPerDay, 200},
		{MetricStorageBytes, 1024},
		{MetricPluginCount, 5},
		{Metric("unknown"), 0},
	}
	for _, tc := range cases {
		t.Run(string(tc.metric), func(t *testing.T) {
			t.Parallel()
			if got := p.Limit(tc.metric); got != tc.want {
				t.Fatalf("Limit(%s) = %d, want %d", tc.metric, got, tc.want)
			}
		})
	}
}

func TestEnforcerCheckAndIncrement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	enf := NewInMemoryEnforcer(func() time.Time { return now })
	policy := PolicyFromLimits(2, 0, 0, 0)
	if err := enf.CheckAndIncrement(ctx, "tenant-a", MetricAPIPerMinute, 1, policy); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := enf.CheckAndIncrement(ctx, "tenant-a", MetricAPIPerMinute, 1, policy); err != nil {
		t.Fatalf("second call: %v", err)
	}
	err := enf.CheckAndIncrement(ctx, "tenant-a", MetricAPIPerMinute, 1, policy)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}
}

func TestEnforcerNoLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	enf := NewInMemoryEnforcer(nil)
	if err := enf.CheckAndIncrement(ctx, "tenant-a", MetricAPIPerMinute, 1, Policy{}); err != nil {
		t.Fatalf("expected ok with zero limit, got %v", err)
	}
}

func TestEnforcerTenantRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	enf := NewInMemoryEnforcer(nil)
	err := enf.CheckAndIncrement(ctx, "", MetricAPIPerMinute, 1, PolicyFromLimits(60, 0, 0, 0))
	if !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired, got %v", err)
	}
}

func TestEnforcerBucketsRotateByMinute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	enf := NewInMemoryEnforcer(func() time.Time { return clock })
	policy := PolicyFromLimits(1, 0, 0, 0)
	if err := enf.CheckAndIncrement(ctx, "tenant-a", MetricAPIPerMinute, 1, policy); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := enf.CheckAndIncrement(ctx, "tenant-a", MetricAPIPerMinute, 1, policy); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected exceeded same minute: %v", err)
	}
	clock = clock.Add(time.Minute) // shift to next bucket
	if err := enf.CheckAndIncrement(ctx, "tenant-a", MetricAPIPerMinute, 1, policy); err != nil {
		t.Fatalf("next minute should reset: %v", err)
	}
}

func TestEnforcerAgentRunsDailyBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	enf := NewInMemoryEnforcer(func() time.Time { return clock })
	policy := PolicyFromLimits(0, 1, 0, 0)
	if err := enf.CheckAndIncrement(ctx, "tenant-a", MetricAgentRunsPerDay, 1, policy); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := enf.CheckAndIncrement(ctx, "tenant-a", MetricAgentRunsPerDay, 1, policy); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected exceeded same day: %v", err)
	}
	clock = clock.Add(24 * time.Hour)
	if err := enf.CheckAndIncrement(ctx, "tenant-a", MetricAgentRunsPerDay, 1, policy); err != nil {
		t.Fatalf("new day should reset: %v", err)
	}
}

func TestEnforcerSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	enf := NewInMemoryEnforcer(func() time.Time { return now })
	policy := PolicyFromLimits(60, 0, 0, 0)
	_ = enf.CheckAndIncrement(ctx, "tenant-a", MetricAPIPerMinute, 1, policy)
	if v := enf.Snapshot("tenant-a", MetricAPIPerMinute); v != 1 {
		t.Fatalf("Snapshot = %d, want 1", v)
	}
}

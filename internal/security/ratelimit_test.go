package security

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryTokenBucketAllowsThenDenies(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	limiter := NewInMemoryTokenBucket(TokenBucketConfig{
		Capacity:       2,
		RefillInterval: time.Minute,
		Now:            func() time.Time { return now },
	})

	for i := 0; i < 2; i++ {
		decision, err := limiter.Allow(context.Background(), "ip:203.0.113.10")
		if err != nil {
			t.Fatalf("Allow #%d: %v", i+1, err)
		}
		if !decision.Allowed {
			t.Fatalf("Allow #%d denied, want allowed", i+1)
		}
	}

	decision, err := limiter.Allow(context.Background(), "ip:203.0.113.10")
	if err != nil {
		t.Fatalf("Allow denied: %v", err)
	}
	if decision.Allowed {
		t.Fatal("third Allow allowed, want denied")
	}
	if decision.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %s, want positive", decision.RetryAfter)
	}
}

func TestInMemoryTokenBucketRefillsDeterministically(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	limiter := NewInMemoryTokenBucket(TokenBucketConfig{
		Capacity:       1,
		RefillInterval: time.Minute,
		Now:            func() time.Time { return now },
	})

	if decision, _ := limiter.Allow(context.Background(), "api-key:test"); !decision.Allowed {
		t.Fatal("first request denied")
	}
	if decision, _ := limiter.Allow(context.Background(), "api-key:test"); decision.Allowed {
		t.Fatal("second request allowed before refill")
	}

	now = now.Add(time.Minute)
	if decision, _ := limiter.Allow(context.Background(), "api-key:test"); !decision.Allowed {
		t.Fatalf("request after refill denied: retry_after=%s", decision.RetryAfter)
	}
}

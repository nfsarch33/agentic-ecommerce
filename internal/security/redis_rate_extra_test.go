package security

import (
	"bufio"
	"context"
	"strings"
	"testing"
	"time"
)

// File scope: extra coverage for the bespoke Redis RESP client driving the
// distributed rate limiter. The fake server pattern reuses the existing
// `startRedisRateServer` helper from redis_rate_test.go.

func TestRedisReadValueParsesSimpleString(t *testing.T) {
	t.Parallel()

	value, err := redisReadValue(bufio.NewReader(strings.NewReader("+OK\r\n")))
	if err != nil {
		t.Fatalf("redisReadValue: %v", err)
	}
	if got, ok := value.(string); !ok || got != "OK" {
		t.Fatalf("value = %#v, want OK", value)
	}
}

func TestRedisReadValueParsesIntegerReply(t *testing.T) {
	t.Parallel()

	value, err := redisReadValue(bufio.NewReader(strings.NewReader(":42\r\n")))
	if err != nil {
		t.Fatalf("redisReadValue: %v", err)
	}
	if got, ok := value.(int64); !ok || got != 42 {
		t.Fatalf("value = %#v, want 42", value)
	}
}

func TestRedisReadValueParsesBulkString(t *testing.T) {
	t.Parallel()

	value, err := redisReadValue(bufio.NewReader(strings.NewReader("$5\r\nhello\r\n")))
	if err != nil {
		t.Fatalf("redisReadValue: %v", err)
	}
	if got, ok := value.(string); !ok || got != "hello" {
		t.Fatalf("value = %#v, want hello", value)
	}
}

func TestRedisReadValueReturnsEmptyForNullBulkString(t *testing.T) {
	t.Parallel()

	value, err := redisReadValue(bufio.NewReader(strings.NewReader("$-1\r\n")))
	if err != nil {
		t.Fatalf("redisReadValue: %v", err)
	}
	if got, ok := value.(string); !ok || got != "" {
		t.Fatalf("value = %#v, want empty string for null bulk", value)
	}
}

func TestRedisReadValueRejectsUnknownPrefix(t *testing.T) {
	t.Parallel()

	if _, err := redisReadValue(bufio.NewReader(strings.NewReader("?bad\r\n"))); err == nil {
		t.Fatal("expected error for unknown prefix")
	}
}

func TestRedisTokenBucketAllowReturnsErrorForMalformedScriptResponse(t *testing.T) {
	t.Parallel()

	addr := startRedisRateServer(t, func(cmd []string) string {
		switch cmd[0] {
		case "SELECT":
			return "+OK\r\n"
		case "EVAL":
			// Returns a single integer rather than a [allowed, retry] array.
			return ":99\r\n"
		default:
			return "-ERR unexpected\r\n"
		}
	})

	limiter := NewRedisTokenBucket(addr, "1", TokenBucketConfig{
		Capacity:       2,
		RefillInterval: 200 * time.Millisecond,
	})
	if _, err := limiter.Allow(context.Background(), "k"); err == nil {
		t.Fatal("expected error for malformed EVAL response shape")
	}
}

func TestRedisTokenBucketAllowReportsSelectFailure(t *testing.T) {
	t.Parallel()

	addr := startRedisRateServer(t, func(cmd []string) string {
		switch cmd[0] {
		case "SELECT":
			return "-ERR no such db\r\n"
		default:
			return "-ERR unreachable\r\n"
		}
	})

	limiter := NewRedisTokenBucket(addr, "9", TokenBucketConfig{Capacity: 5, RefillInterval: time.Second})
	if _, err := limiter.Allow(context.Background(), "k"); err == nil {
		t.Fatal("expected SELECT failure to surface as Allow error")
	}
}

func TestRedisTokenBucketAllowReportsDialFailureFastWithDeadline(t *testing.T) {
	t.Parallel()

	limiter := NewRedisTokenBucket("127.0.0.1:1", "0", TokenBucketConfig{
		Capacity:       1,
		RefillInterval: time.Second,
		RedisTimeout:   200 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := limiter.Allow(ctx, "k"); err == nil {
		t.Fatal("expected dial failure when port 1 is closed")
	}
	if elapsed := time.Since(start); elapsed > 700*time.Millisecond {
		t.Fatalf("Allow took %s, want fast dial failure", elapsed)
	}
}

func TestRedisTokenBucketAllowAllowsRequestWhenScriptReturnsAllowed(t *testing.T) {
	t.Parallel()

	addr := startRedisRateServer(t, func(cmd []string) string {
		switch cmd[0] {
		case "SELECT":
			return "+OK\r\n"
		case "EVAL":
			return "*2\r\n:1\r\n:0\r\n"
		default:
			return "-ERR unexpected\r\n"
		}
	})

	limiter := NewRedisTokenBucket(addr, "1", TokenBucketConfig{
		Capacity:       3,
		RefillInterval: 500 * time.Millisecond,
	})
	decision, err := limiter.Allow(context.Background(), "user-7")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision = %+v, want allowed", decision)
	}
	if decision.RetryAfter != 0 {
		t.Fatalf("decision.RetryAfter = %s, want 0 when allowed", decision.RetryAfter)
	}
}

func TestRedisTokenBucketAllowDefaultsKeyToAnonymousWhenEmpty(t *testing.T) {
	t.Parallel()

	captured := make(chan []string, 2)
	addr := startRedisRateServer(t, func(cmd []string) string {
		captured <- cmd
		switch cmd[0] {
		case "SELECT":
			return "+OK\r\n"
		case "EVAL":
			return "*2\r\n:1\r\n:0\r\n"
		default:
			return "-ERR unexpected\r\n"
		}
	})

	limiter := NewRedisTokenBucket(addr, "1", TokenBucketConfig{Capacity: 1, RefillInterval: time.Second})
	if _, err := limiter.Allow(context.Background(), ""); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	<-captured
	evalCmd := <-captured
	const wantKey = "rate:anonymous"
	if len(evalCmd) < 4 || evalCmd[3] != wantKey {
		t.Fatalf("eval key = %q, want %q", evalCmd[3], wantKey)
	}
}

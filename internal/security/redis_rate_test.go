package security

import (
	"bufio"
	"strings"
	"testing"
	"time"
)

func TestNewRedisTokenBucketAppliesDefaults(t *testing.T) {
	t.Parallel()
	limiter := NewRedisTokenBucket("127.0.0.1:6379", "0", TokenBucketConfig{})
	if limiter.capacity != 60 {
		t.Fatalf("capacity = %d, want 60", limiter.capacity)
	}
	if limiter.refillInterval != time.Minute {
		t.Fatalf("refillInterval = %s, want 1m", limiter.refillInterval)
	}
}

func TestRedisReadValueParsesArray(t *testing.T) {
	t.Parallel()
	reader := bufio.NewReader(strings.NewReader("*2\r\n:1\r\n:250\r\n"))

	value, err := redisReadValue(reader)
	if err != nil {
		t.Fatalf("redisReadValue: %v", err)
	}
	items, ok := value.([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("value = %#v, want two-item array", value)
	}
	if items[0].(int64) != 1 || items[1].(int64) != 250 {
		t.Fatalf("items = %#v", items)
	}
}

func TestRedisReadValueReturnsServerError(t *testing.T) {
	t.Parallel()
	reader := bufio.NewReader(strings.NewReader("-ERR nope\r\n"))
	if _, err := redisReadValue(reader); err == nil || !strings.Contains(err.Error(), "ERR nope") {
		t.Fatalf("error = %v, want Redis error", err)
	}
}

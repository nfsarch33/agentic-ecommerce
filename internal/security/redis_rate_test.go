package security

import (
	"bufio"
	"context"
	"io"
	"net"
	"strconv"
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

func TestRedisTokenBucketAllowUsesRedisScriptResponse(t *testing.T) {
	t.Parallel()

	commands := make(chan []string, 2)
	addr := startRedisRateServer(t, func(cmd []string) string {
		commands <- cmd
		switch cmd[0] {
		case "SELECT":
			return "+OK\r\n"
		case "EVAL":
			return "*2\r\n:0\r\n:750\r\n"
		default:
			return "-ERR unexpected command\r\n"
		}
	})
	limiter := NewRedisTokenBucket(addr, "2", TokenBucketConfig{
		Capacity:       3,
		RefillInterval: 750 * time.Millisecond,
		Now:            func() time.Time { return time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC) },
	})

	decision, err := limiter.Allow(context.Background(), "customer:42")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if decision.Allowed || decision.RetryAfter != 750*time.Millisecond {
		t.Fatalf("decision = %+v, want denied with 750ms retry", decision)
	}

	selectCmd := <-commands
	if len(selectCmd) != 2 || selectCmd[0] != "SELECT" || selectCmd[1] != "2" {
		t.Fatalf("select command = %#v", selectCmd)
	}
	evalCmd := <-commands
	if len(evalCmd) < 8 || evalCmd[0] != "EVAL" || evalCmd[2] != "1" || evalCmd[3] != "rate:customer:42" {
		t.Fatalf("eval command = %#v", evalCmd)
	}
	if !strings.Contains(evalCmd[1], "redis.call('HMGET'") {
		t.Fatalf("script missing token bucket logic: %q", evalCmd[1])
	}
}

func TestRedisTokenBucketAllowFallsBackOpenWhenDisabled(t *testing.T) {
	t.Parallel()

	decision, err := (*RedisTokenBucket)(nil).Allow(context.Background(), "")
	if err != nil {
		t.Fatalf("nil limiter Allow: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("nil limiter decision = %+v, want allowed", decision)
	}

	decision, err = NewRedisTokenBucket("", "", TokenBucketConfig{}).Allow(context.Background(), "")
	if err != nil {
		t.Fatalf("empty addr Allow: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("empty addr decision = %+v, want allowed", decision)
	}
}

func startRedisRateServer(t *testing.T, respond func([]string) string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for {
			cmd, err := readRedisRateCommand(reader)
			if err != nil {
				return
			}
			if _, err := conn.Write([]byte(respond(cmd))); err != nil {
				return
			}
		}
	}()

	return listener.Addr().String()
}

func readRedisRateCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(line), "*"))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		sizeLine, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(sizeLine), "$"))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return nil, err
		}
		out = append(out, string(buf[:size]))
	}
	return out, nil
}

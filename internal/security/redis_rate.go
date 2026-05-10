package security

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type RedisTokenBucket struct {
	addr             string
	db               string
	capacity         int
	refillInterval   time.Duration
	operationTimeout time.Duration
	now              func() time.Time
}

func NewRedisTokenBucket(addr, db string, cfg TokenBucketConfig) *RedisTokenBucket {
	capacity := cfg.Capacity
	if capacity <= 0 {
		capacity = 60
	}
	refillInterval := cfg.RefillInterval
	if refillInterval <= 0 {
		refillInterval = time.Minute
	}
	operationTimeout := cfg.RedisTimeout
	if operationTimeout <= 0 {
		operationTimeout = 500 * time.Millisecond
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &RedisTokenBucket{
		addr:             strings.TrimSpace(addr),
		db:               strings.TrimSpace(db),
		capacity:         capacity,
		refillInterval:   refillInterval,
		operationTimeout: operationTimeout,
		now:              now,
	}
}

func (l *RedisTokenBucket) Allow(ctx context.Context, key string) (RateLimitDecision, error) {
	if l == nil || l.addr == "" {
		return RateLimitDecision{Allowed: true}, nil
	}
	if key == "" {
		key = "anonymous"
	}
	rw, closer, err := l.dialRedis(ctx)
	if err != nil {
		return RateLimitDecision{}, err
	}
	defer closer()
	if err := l.selectDB(rw); err != nil {
		return RateLimitDecision{}, err
	}
	return l.evalBucketScript(rw, key)
}

func (l *RedisTokenBucket) dialRedis(ctx context.Context) (*bufio.ReadWriter, func(), error) {
	dialCtx := ctx
	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok {
		dialCtx, cancel = context.WithTimeout(ctx, l.operationTimeout)
	}
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", l.addr)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(l.operationTimeout))
	}
	closer := func() {
		conn.Close()
		if cancel != nil {
			cancel()
		}
	}
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	return rw, closer, nil
}

func (l *RedisTokenBucket) selectDB(rw *bufio.ReadWriter) error {
	if l.db == "" || l.db == "0" {
		return nil
	}
	if err := redisWriteCommand(rw, "SELECT", l.db); err != nil {
		return err
	}
	_, err := redisReadValue(rw.Reader)
	return err
}

func (l *RedisTokenBucket) evalBucketScript(rw *bufio.ReadWriter, key string) (RateLimitDecision, error) {
	script := redisTokenBucketScript()
	nowMillis := strconv.FormatInt(l.now().UTC().UnixMilli(), 10)
	refillMillis := strconv.FormatInt(l.refillInterval.Milliseconds(), 10)
	ttlMillis := strconv.FormatInt((l.refillInterval * time.Duration(l.capacity)).Milliseconds(), 10)
	if err := redisWriteCommand(rw, "EVAL", script, "1", "rate:"+key, strconv.Itoa(l.capacity), refillMillis, nowMillis, ttlMillis); err != nil {
		return RateLimitDecision{}, err
	}
	return parseRateLimitResponse(rw)
}

func parseRateLimitResponse(rw *bufio.ReadWriter) (RateLimitDecision, error) {
	value, err := redisReadValue(rw.Reader)
	if err != nil {
		return RateLimitDecision{}, err
	}
	items, ok := value.([]any)
	if !ok || len(items) != 2 {
		return RateLimitDecision{}, fmt.Errorf("redis rate limit response: %v", value)
	}
	allowed, _ := items[0].(int64)
	retryMillis, _ := items[1].(int64)
	return RateLimitDecision{Allowed: allowed == 1, RetryAfter: time.Duration(retryMillis) * time.Millisecond}, nil
}

func redisTokenBucketScript() string {
	return `
local capacity = tonumber(ARGV[1])
local refill_ms = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])
local ttl_ms = tonumber(ARGV[4])
local bucket = redis.call('HMGET', KEYS[1], 'tokens', 'updated_at')
local tokens = tonumber(bucket[1])
local updated = tonumber(bucket[2])
if tokens == nil or updated == nil then
  tokens = capacity
  updated = now_ms
end
local elapsed = now_ms - updated
if elapsed >= refill_ms then
  local refills = math.floor(elapsed / refill_ms)
  tokens = math.min(capacity, tokens + refills)
  updated = updated + (refills * refill_ms)
end
if tokens <= 0 then
  redis.call('HMSET', KEYS[1], 'tokens', tokens, 'updated_at', updated)
  redis.call('PEXPIRE', KEYS[1], ttl_ms)
  local retry = refill_ms - (now_ms - updated)
  if retry < 1 then retry = refill_ms end
  return {0, retry}
end
tokens = tokens - 1
redis.call('HMSET', KEYS[1], 'tokens', tokens, 'updated_at', updated)
redis.call('PEXPIRE', KEYS[1], ttl_ms)
return {1, 0}
`
}

func redisWriteCommand(rw *bufio.ReadWriter, args ...string) error {
	if _, err := fmt.Fprintf(rw, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(rw, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}
	return rw.Flush()
}

func redisReadValue(r *bufio.Reader) (any, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	switch prefix {
	case '+':
		return line, nil
	case '-':
		return nil, fmt.Errorf("redis error: %s", line)
	case ':':
		return strconv.ParseInt(line, 10, 64)
	case '$':
		size, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if size < 0 {
			return "", nil
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:size]), nil
	case '*':
		count, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, count)
		for i := 0; i < count; i++ {
			item, err := redisReadValue(r)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("redis response prefix %q", prefix)
	}
}

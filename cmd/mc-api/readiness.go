package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type requestIDContextKey struct{}

type readyzResponse struct {
	Status      string                            `json:"status"`
	Service     string                            `json:"service"`
	Agents      int                               `json:"agents"`
	AgentWorker agentWorkerReadiness              `json:"agent_worker"`
	Checks      map[string]readinessCheckResponse `json:"checks"`
}

type agentWorkerReadiness struct {
	Ready            bool   `json:"ready"`
	Scheduler        string `json:"scheduler"`
	RegisteredAgents int    `json:"registered_agents"`
}

type readinessCheckResponse struct {
	Status    string `json:"status"`
	Optional  bool   `json:"optional"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type readinessProbe struct {
	name     string
	optional bool
	check    func(context.Context) error
}

func defaultReadinessChecks() []readinessProbe {
	return []readinessProbe{
		{name: "database", optional: true},
		{name: "redis", optional: true},
	}
}

func newReadinessChecksFromEnv(time.Duration) ([]readinessProbe, []func()) {
	checks := defaultReadinessChecks()
	var cleanup []func()

	if dsn := strings.TrimSpace(os.Getenv("ECOMMERCE_DB_URL")); dsn != "" {
		pool, err := pgxpool.New(context.Background(), dsn)
		checks[0] = readinessProbe{
			name:     "database",
			optional: false,
			check: func(ctx context.Context) error {
				if err != nil {
					return err
				}
				return pool.Ping(ctx)
			},
		}
		if pool != nil {
			cleanup = append(cleanup, pool.Close)
		}
	}

	if addr := strings.TrimSpace(os.Getenv("ECOMMERCE_REDIS_ADDR")); addr != "" {
		db := strings.TrimSpace(os.Getenv("ECOMMERCE_REDIS_DB"))
		checks[1] = readinessProbe{
			name:     "redis",
			optional: false,
			check: func(ctx context.Context) error {
				return pingRedis(ctx, addr, db)
			},
		}
	}

	return checks, cleanup
}

func (s *server) runReadinessChecks(ctx context.Context) map[string]readinessCheckResponse {
	timeout := s.cfg.readinessTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	checks := s.readiness
	if len(checks) == 0 {
		checks = defaultReadinessChecks()
	}

	results := make(map[string]readinessCheckResponse, len(checks))
	for _, probe := range checks {
		start := time.Now()
		result := readinessCheckResponse{
			Status:    "skipped",
			Optional:  probe.optional,
			LatencyMS: 0,
		}
		if probe.check != nil {
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			err := probe.check(checkCtx)
			cancel()
			result.LatencyMS = time.Since(start).Milliseconds()
			if err != nil {
				result.Status = "fail"
				result.Error = "check_failed"
			} else {
				result.Status = "ok"
			}
		}
		results[probe.name] = result
	}
	return results
}

func hasFailedReadinessCheck(checks map[string]readinessCheckResponse) bool {
	for _, check := range checks {
		if check.Status == "fail" {
			return true
		}
	}
	return false
}

func pingRedis(ctx context.Context, addr, db string) error {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	if db != "" && db != "0" {
		if _, err := strconv.Atoi(db); err != nil {
			return fmt.Errorf("redis db: %w", err)
		}
		if err := redisCommand(rw, "SELECT", db); err != nil {
			return err
		}
		if err := readRedisSimpleResponse(rw.Reader, "OK"); err != nil {
			return err
		}
	}
	if err := redisCommand(rw, "PING"); err != nil {
		return err
	}
	return readRedisSimpleResponse(rw.Reader, "PONG")
}

func redisCommand(rw *bufio.ReadWriter, args ...string) error {
	if len(args) == 0 {
		return errors.New("redis command requires args")
	}
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

func readRedisSimpleResponse(r *bufio.Reader, want string) error {
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if line == "+"+want {
		return nil
	}
	if strings.HasPrefix(line, "-") {
		return fmt.Errorf("redis error: %s", strings.TrimPrefix(line, "-"))
	}
	return fmt.Errorf("redis response %q, want +%s", line, want)
}

func parseDurationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parseBoolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

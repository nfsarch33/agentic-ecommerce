package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// doctorCheck represents a single environment probe.
type doctorCheck struct {
	Name     string
	Required bool
	Status   string
	Detail   string
	OK       bool
}

// doctorReport bundles every probe outcome plus an overall verdict.
// JSON-friendly shape so other tooling can consume the output.
type doctorReport struct {
	Generated string        `json:"generated"`
	Healthy   bool          `json:"healthy"`
	Checks    []doctorCheck `json:"checks"`
}

// runDoctor executes the environment diagnostics. Returns 0 when
// every required check passes; 1 otherwise.
func runDoctor(ctx context.Context, args []string, deps appDeps) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(deps.stderr)
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	timeoutSec := fs.Int("timeout-seconds", 5, "per-check timeout in seconds")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	timeout := time.Duration(*timeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	report := doctorReport{Generated: deps.now()}
	report.Checks = append(report.Checks, checkRequiredEnv(deps))
	report.Checks = append(report.Checks, checkPostgres(ctx, deps, timeout))
	report.Checks = append(report.Checks, checkRedis(ctx, deps, timeout))
	report.Checks = append(report.Checks, checkTemporal(ctx, deps, timeout))
	report.Checks = append(report.Checks, checkAPI(ctx, deps, timeout))
	report.Healthy = allRequiredHealthy(report.Checks)

	if *jsonOut {
		return writeJSONReport(deps, report)
	}
	return writeTextReport(deps, report)
}

func writeJSONReport(deps appDeps, report doctorReport) int {
	if err := encodeJSON(deps.stdout, report); err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli doctor: encode json: %v\n", err)
		return 1
	}
	if !report.Healthy {
		return 1
	}
	return 0
}

func writeTextReport(deps appDeps, report doctorReport) int {
	fmt.Fprintf(deps.stdout, "ec-cli doctor (generated %s)\n", report.Generated)
	for _, c := range report.Checks {
		marker := "OK"
		if !c.OK {
			marker = "FAIL"
			if !c.Required {
				marker = "WARN"
			}
		}
		fmt.Fprintf(deps.stdout, "  [%s] %-12s %s -- %s\n", marker, c.Status, c.Name, c.Detail)
	}
	if report.Healthy {
		fmt.Fprintf(deps.stdout, "verdict: HEALTHY\n")
		return 0
	}
	fmt.Fprintf(deps.stdout, "verdict: UNHEALTHY\n")
	return 1
}

func allRequiredHealthy(checks []doctorCheck) bool {
	for _, c := range checks {
		if c.Required && !c.OK {
			return false
		}
	}
	return true
}

func checkRequiredEnv(deps appDeps) doctorCheck {
	required := []string{
		"ECOMMERCE_DB_URL",
		"ECOMMERCE_REDIS_URL",
		"ECOMMERCE_TEMPORAL_ADDR",
	}
	missing := make([]string, 0)
	for _, key := range required {
		if trimEmpty(deps.getenv(key)) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return doctorCheck{
			Name:     "env",
			Required: true,
			Status:   "missing",
			Detail:   "missing: " + strings.Join(missing, ","),
			OK:       false,
		}
	}
	return doctorCheck{
		Name:     "env",
		Required: true,
		Status:   "set",
		Detail:   "all required env vars present",
		OK:       true,
	}
}

func checkPostgres(ctx context.Context, deps appDeps, timeout time.Duration) doctorCheck {
	dsn := trimEmpty(deps.getenv("ECOMMERCE_DB_URL"))
	check := doctorCheck{Name: "postgres", Required: true}
	if dsn == "" {
		check.Status = "skip"
		check.Detail = "ECOMMERCE_DB_URL not set"
		return check
	}
	host, port, ok := hostPortFromPostgresDSN(dsn)
	if !ok {
		check.Status = "invalid"
		check.Detail = "could not parse host:port from ECOMMERCE_DB_URL"
		return check
	}
	if err := dialTCP(ctx, host, port, timeout); err != nil {
		check.Status = "down"
		check.Detail = fmt.Sprintf("dial %s:%s: %v", host, port, err)
		return check
	}
	check.Status = "up"
	check.Detail = fmt.Sprintf("dial %s:%s: ok", host, port)
	check.OK = true
	return check
}

func checkRedis(ctx context.Context, deps appDeps, timeout time.Duration) doctorCheck {
	url := trimEmpty(deps.getenv("ECOMMERCE_REDIS_URL"))
	check := doctorCheck{Name: "redis", Required: false}
	if url == "" {
		check.Status = "skip"
		check.Detail = "ECOMMERCE_REDIS_URL not set"
		return check
	}
	host, port, ok := hostPortFromRedisURL(url)
	if !ok {
		check.Status = "invalid"
		check.Detail = "could not parse host:port from ECOMMERCE_REDIS_URL"
		return check
	}
	if err := dialTCP(ctx, host, port, timeout); err != nil {
		check.Status = "down"
		check.Detail = fmt.Sprintf("dial %s:%s: %v", host, port, err)
		return check
	}
	check.Status = "up"
	check.Detail = fmt.Sprintf("dial %s:%s: ok", host, port)
	check.OK = true
	return check
}

func checkTemporal(ctx context.Context, deps appDeps, timeout time.Duration) doctorCheck {
	addr := trimEmpty(deps.getenv("ECOMMERCE_TEMPORAL_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:7233"
	}
	check := doctorCheck{Name: "temporal", Required: false}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		check.Status = "invalid"
		check.Detail = fmt.Sprintf("ECOMMERCE_TEMPORAL_ADDR=%q: %v", addr, err)
		return check
	}
	if err := dialTCP(ctx, host, port, timeout); err != nil {
		check.Status = "down"
		check.Detail = fmt.Sprintf("dial %s:%s: %v", host, port, err)
		return check
	}
	check.Status = "up"
	check.Detail = fmt.Sprintf("dial %s:%s: ok", host, port)
	check.OK = true
	return check
}

func checkAPI(ctx context.Context, deps appDeps, timeout time.Duration) doctorCheck {
	base := apiBaseURL(deps)
	check := doctorCheck{Name: "mc-api", Required: false}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		check.Status = "invalid"
		check.Detail = err.Error()
		return check
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		check.Status = "down"
		check.Detail = fmt.Sprintf("GET %s/healthz: %v", base, err)
		return check
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		check.Status = "unhealthy"
		check.Detail = fmt.Sprintf("GET %s/healthz: status %d", base, resp.StatusCode)
		return check
	}
	check.Status = "up"
	check.Detail = fmt.Sprintf("GET %s/healthz: 200", base)
	check.OK = true
	return check
}

func apiBaseURL(deps appDeps) string {
	if v := trimEmpty(deps.getenv("EC_API_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://127.0.0.1:8080"
}

func dialTCP(ctx context.Context, host, port string, timeout time.Duration) error {
	if host == "" {
		return fmt.Errorf("empty host")
	}
	d := net.Dialer{Timeout: timeout}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := d.DialContext(dialCtx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func hostPortFromPostgresDSN(dsn string) (string, string, bool) {
	if u, err := url.Parse(dsn); err == nil && u.Host != "" {
		host := u.Hostname()
		port := u.Port()
		if port == "" {
			port = "5432"
		}
		return host, port, true
	}
	for _, kv := range strings.Fields(dsn) {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] == "host" {
			host := parts[1]
			port := "5432"
			for _, kv2 := range strings.Fields(dsn) {
				p := strings.SplitN(kv2, "=", 2)
				if len(p) == 2 && p[0] == "port" {
					port = p[1]
				}
			}
			return host, port, true
		}
	}
	return "", "", false
}

func hostPortFromRedisURL(raw string) (string, string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", "", false
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "6379"
	}
	return host, port, true
}

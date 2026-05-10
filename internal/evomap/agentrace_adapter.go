package evomap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// AgentraceInsight mirrors a single insight record from the Agentrace
// loopback API (GET 127.0.0.1:8100/api/insights) or from the NDJSON
// event file (~/.agentrace/events.jsonl).
type AgentraceInsight struct {
	Type      string    `json:"type"`
	SessionID string    `json:"session_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Tool      string    `json:"tool,omitempty"`
	Outcome   string    `json:"outcome,omitempty"`
	CostUSD   float64   `json:"cost_usd,omitempty"`
	Severity  string    `json:"severity,omitempty"`
	Ratio     float64   `json:"ratio,omitempty"`
	DurationS float64   `json:"duration_s,omitempty"`
}

// AgentraceKPIs holds the derived KPIs ready for EvoMap capsule
// emission. Added as additive fields on the existing KPIs struct via
// Story 1 field extension.
type AgentraceKPIs struct {
	Available          bool           `json:"agentrace_available"`
	SessionDurationSec float64        `json:"agentrace_session_duration_seconds,omitempty"`
	ToolCallCount      int            `json:"agentrace_tool_call_count,omitempty"`
	CostUSD            float64        `json:"agentrace_cost_usd,omitempty"`
	BottleneckCount    int            `json:"agentrace_bottleneck_count,omitempty"`
	ParallelismRatio   float64        `json:"agentrace_parallelism_efficiency,omitempty"`
	ToolUsage          map[string]int `json:"agentrace_tool_usage,omitempty"`
	ToolErrors         map[string]int `json:"agentrace_tool_errors,omitempty"`
}

// AgentraceAdapterConfig configures the adapter.
type AgentraceAdapterConfig struct {
	HTTPURL     string
	JSONLPath   string
	HTTPTimeout time.Duration
	HTTPClient  HTTPDoer
	Logger      *slog.Logger
}

// HTTPDoer abstracts http.Client for testing.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// AgentraceAdapter reads Agentrace insights and transforms them into
// EvoMap-compatible KPIs.
type AgentraceAdapter struct {
	cfg AgentraceAdapterConfig
}

// NewAgentraceAdapter constructs an adapter with defaults applied.
func NewAgentraceAdapter(cfg AgentraceAdapterConfig) *AgentraceAdapter {
	if cfg.HTTPURL == "" {
		cfg.HTTPURL = "http://127.0.0.1:8100/api/insights"
	}
	if cfg.JSONLPath == "" {
		home, _ := os.UserHomeDir()
		cfg.JSONLPath = home + "/.agentrace/events.jsonl"
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = 2 * time.Second
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.HTTPTimeout}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &AgentraceAdapter{cfg: cfg}
}

// Read fetches Agentrace insights. It tries the HTTP loopback first;
// on connection failure it falls back to the JSONL file. If neither
// source is available it returns a zero-value KPI set with
// Available=false.
func (a *AgentraceAdapter) Read(ctx context.Context) AgentraceKPIs {
	insights, err := a.tryHTTP(ctx)
	if err != nil {
		a.cfg.Logger.Debug("agentrace HTTP unavailable, trying JSONL fallback",
			"error", err)
		insights, err = a.fallbackJSONL()
	}
	if err != nil {
		a.cfg.Logger.Debug("agentrace JSONL fallback unavailable",
			"error", err)
		return AgentraceKPIs{Available: false}
	}
	return a.transformInsights(insights)
}

func (a *AgentraceAdapter) tryHTTP(ctx context.Context) ([]AgentraceInsight, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.HTTPURL, nil)
	if err != nil {
		return nil, fmt.Errorf("agentrace: build request: %w", err)
	}
	resp, err := a.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agentrace: http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agentrace: http status %d", resp.StatusCode)
	}
	var insights []AgentraceInsight
	if err := json.NewDecoder(resp.Body).Decode(&insights); err != nil {
		return nil, fmt.Errorf("agentrace: decode: %w", err)
	}
	return insights, nil
}

func (a *AgentraceAdapter) fallbackJSONL() ([]AgentraceInsight, error) {
	f, err := os.Open(a.cfg.JSONLPath)
	if err != nil {
		return nil, fmt.Errorf("agentrace: open jsonl: %w", err)
	}
	defer f.Close()
	return parseJSONLInsights(f)
}

func parseJSONLInsights(r io.Reader) ([]AgentraceInsight, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var insights []AgentraceInsight
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ins AgentraceInsight
		if err := json.Unmarshal([]byte(line), &ins); err != nil {
			continue
		}
		insights = append(insights, ins)
	}
	return insights, scanner.Err()
}

func (a *AgentraceAdapter) transformInsights(insights []AgentraceInsight) AgentraceKPIs {
	kpis := AgentraceKPIs{
		Available:  true,
		ToolUsage:  make(map[string]int),
		ToolErrors: make(map[string]int),
	}
	sessions := make(map[string][2]time.Time) // [start, end]
	for _, ins := range insights {
		switch ins.Type {
		case "session_start", "session_end":
			accumulateSession(ins, sessions)
		case "tool_call":
			kpis.ToolCallCount++
			if ins.Tool != "" {
				kpis.ToolUsage[ins.Tool]++
			}
			if ins.Outcome == "error" && ins.Tool != "" {
				kpis.ToolErrors[ins.Tool]++
			}
		case "cost":
			kpis.CostUSD += ins.CostUSD
		case "bottleneck":
			kpis.BottleneckCount++
		case "parallelism":
			kpis.ParallelismRatio = ins.Ratio
		}
	}
	kpis.SessionDurationSec = sumSessionDurations(sessions)
	return kpis
}

func accumulateSession(ins AgentraceInsight, sessions map[string][2]time.Time) {
	sid := ins.SessionID
	if sid == "" {
		return
	}
	pair := sessions[sid]
	if ins.Type == "session_start" {
		pair[0] = ins.Timestamp
	} else {
		pair[1] = ins.Timestamp
	}
	sessions[sid] = pair
}

func sumSessionDurations(sessions map[string][2]time.Time) float64 {
	var total float64
	for _, pair := range sessions {
		if !pair[0].IsZero() && !pair[1].IsZero() {
			total += pair[1].Sub(pair[0]).Seconds()
		}
	}
	return total
}

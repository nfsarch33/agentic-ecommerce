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
	"sort"
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
	Available          bool                `json:"agentrace_available"`
	SessionDurationSec float64             `json:"agentrace_session_duration_seconds,omitempty"`
	ToolCallCount      int                 `json:"agentrace_tool_call_count,omitempty"`
	CostUSD            float64             `json:"agentrace_cost_usd,omitempty"`
	BottleneckCount    int                 `json:"agentrace_bottleneck_count,omitempty"`
	ParallelismRatio   float64             `json:"agentrace_parallelism_efficiency,omitempty"`
	ToolUsage          map[string]int      `json:"agentrace_tool_usage,omitempty"`
	ToolErrors         map[string]int      `json:"agentrace_tool_errors,omitempty"`
	Stories            []AgentraceStoryKPI `json:"agentrace_stories,omitempty"`
}

type AgentraceStoryKPI struct {
	SessionID      string  `json:"session_id,omitempty"`
	SprintID       string  `json:"sprint_id,omitempty"`
	StoryID        string  `json:"story_id,omitempty"`
	Repo           string  `json:"repo,omitempty"`
	Branch         string  `json:"branch,omitempty"`
	RemoteTarget   string  `json:"remote_target,omitempty"`
	BlockedReason  string  `json:"blocked_reason,omitempty"`
	WallSeconds    float64 `json:"wall_seconds,omitempty"`
	ActiveSeconds  float64 `json:"active_seconds,omitempty"`
	BlockedSeconds float64 `json:"blocked_seconds,omitempty"`
	Outcome        string  `json:"outcome,omitempty"`
}

// AgentraceAdapterConfig configures the adapter.
type AgentraceAdapterConfig struct {
	HTTPURL     string
	JSONLPath   string
	HTTPTimeout time.Duration
	HTTPClient  HTTPDoer
	Logger      *slog.Logger
	PreferJSONL bool
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
	if a.cfg.PreferJSONL {
		return a.fallbackJSONL()
	}
	insights, err := a.tryHTTP(ctx)
	if err != nil {
		a.cfg.Logger.Debug("agentrace HTTP unavailable, trying JSONL fallback",
			"error", err)
		return a.fallbackJSONL()
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

func (a *AgentraceAdapter) fallbackJSONL() AgentraceKPIs {
	f, err := os.Open(a.cfg.JSONLPath)
	if err != nil {
		a.cfg.Logger.Debug("agentrace JSONL fallback unavailable", "error", fmt.Errorf("agentrace: open jsonl: %w", err))
		return AgentraceKPIs{Available: false}
	}
	defer f.Close()
	rawEvents, insights, err := parseJSONLRecords(f)
	if err != nil {
		a.cfg.Logger.Debug("agentrace JSONL fallback unavailable", "error", err)
		return AgentraceKPIs{Available: false}
	}
	if len(rawEvents) > 0 {
		return transformRawEvents(rawEvents)
	}
	if len(insights) > 0 {
		return a.transformInsights(insights)
	}
	return AgentraceKPIs{Available: false}
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

type rawAgentraceEvent struct {
	Type       string         `json:"type"`
	Timestamp  int64          `json:"timestamp"`
	SessionID  string         `json:"session_id,omitempty"`
	AgentID    string         `json:"agent_id,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	Error      string         `json:"error,omitempty"`
	CostUSD    float64        `json:"cost_usd,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

func parseJSONLRecords(r io.Reader) ([]rawAgentraceEvent, []AgentraceInsight, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var rawEvents []rawAgentraceEvent
	var insights []AgentraceInsight
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw rawAgentraceEvent
		if err := json.Unmarshal([]byte(line), &raw); err == nil && raw.Type != "" && (raw.Timestamp != 0 || raw.SessionID != "" || raw.ToolCallID != "") {
			rawEvents = append(rawEvents, raw)
			continue
		}
		var ins AgentraceInsight
		if err := json.Unmarshal([]byte(line), &ins); err == nil && ins.Type != "" {
			insights = append(insights, ins)
		}
	}
	return rawEvents, insights, scanner.Err()
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

type storyInterval struct {
	start int64
	end   int64
}

type runningTool struct {
	startMs  int64
	toolName string
}

type storyAccumulator struct {
	SessionID    string
	SprintID     string
	StoryID      string
	Repo         string
	Branch       string
	RemoteTarget string

	BlockedReason string
	HasPrompt     bool
	PromptMs      int64
	HasStop       bool
	StopMs        int64
	FirstMs       int64
	LastMs        int64
	Intervals     []storyInterval
}

func transformRawEvents(events []rawAgentraceEvent) AgentraceKPIs {
	sort.Slice(events, func(i, j int) bool {
		if events[i].Timestamp == events[j].Timestamp {
			return events[i].Type < events[j].Type
		}
		return events[i].Timestamp < events[j].Timestamp
	})

	kpis := AgentraceKPIs{
		Available:  true,
		ToolUsage:  make(map[string]int),
		ToolErrors: make(map[string]int),
	}

	stories := make(map[string]*storyAccumulator)
	running := make(map[string]runningTool)
	for _, event := range events {
		sessionID := strings.TrimSpace(event.SessionID)
		if sessionID == "" {
			continue
		}
		story := ensureStoryAccumulator(stories, sessionID)
		story.observeEvent(event)
		if event.CostUSD != 0 {
			kpis.CostUSD += event.CostUSD
		}

		switch event.Type {
		case "UserPromptSubmit":
			story.HasPrompt = true
			story.PromptMs = event.Timestamp
		case "PreToolUse":
			kpis.ToolCallCount++
			if tool := strings.TrimSpace(event.ToolName); tool != "" {
				kpis.ToolUsage[tool]++
			}
			if event.ToolCallID != "" {
				running[storyToolKey(sessionID, event.ToolCallID)] = runningTool{
					startMs:  event.Timestamp,
					toolName: strings.TrimSpace(event.ToolName),
				}
			}
		case "PostToolUse", "PostToolUseFailure":
			if event.ToolCallID != "" {
				key := storyToolKey(sessionID, event.ToolCallID)
				if tool, ok := running[key]; ok {
					story.Intervals = append(story.Intervals, storyInterval{
						start: tool.startMs,
						end:   maxInt64(tool.startMs, event.Timestamp),
					})
					if event.Type == "PostToolUseFailure" && tool.toolName != "" {
						kpis.ToolErrors[tool.toolName]++
					}
					delete(running, key)
				}
			}
		case "Stop":
			story.HasStop = true
			story.StopMs = event.Timestamp
		}
	}

	sessionIDs := make([]string, 0, len(stories))
	for sessionID := range stories {
		sessionIDs = append(sessionIDs, sessionID)
	}
	sort.Strings(sessionIDs)
	for _, sessionID := range sessionIDs {
		story := stories[sessionID]
		wall := story.wallSeconds()
		active := story.activeSeconds()
		if active > wall {
			active = wall
		}
		blocked := wall - active
		if blocked < 0 {
			blocked = 0
		}
		kpis.SessionDurationSec += wall
		kpis.Stories = append(kpis.Stories, AgentraceStoryKPI{
			SessionID:      story.SessionID,
			SprintID:       story.SprintID,
			StoryID:        story.StoryID,
			Repo:           story.Repo,
			Branch:         story.Branch,
			RemoteTarget:   story.RemoteTarget,
			BlockedReason:  story.BlockedReason,
			WallSeconds:    wall,
			ActiveSeconds:  active,
			BlockedSeconds: blocked,
			Outcome:        story.outcome(),
		})
	}
	return kpis
}

func ensureStoryAccumulator(stories map[string]*storyAccumulator, sessionID string) *storyAccumulator {
	if story, ok := stories[sessionID]; ok {
		return story
	}
	story := &storyAccumulator{SessionID: sessionID}
	stories[sessionID] = story
	return story
}

func (s *storyAccumulator) observeEvent(event rawAgentraceEvent) {
	if s.FirstMs == 0 || event.Timestamp < s.FirstMs {
		s.FirstMs = event.Timestamp
	}
	if event.Timestamp > s.LastMs {
		s.LastMs = event.Timestamp
	}
	s.setMetadata(event.Payload)
}

func (s *storyAccumulator) setMetadata(payload map[string]any) {
	if value := payloadString(payload, "sprint_id"); value != "" {
		s.SprintID = value
	}
	if value := payloadString(payload, "story_id"); value != "" {
		s.StoryID = value
	}
	if value := payloadString(payload, "repo"); value != "" {
		s.Repo = value
	}
	if value := payloadString(payload, "branch"); value != "" {
		s.Branch = value
	}
	if value := payloadString(payload, "remote_target"); value != "" {
		s.RemoteTarget = value
	}
	if value := payloadString(payload, "blocked_reason"); value != "" {
		s.BlockedReason = value
	}
}

func (s *storyAccumulator) wallSeconds() float64 {
	start := s.FirstMs
	if s.HasPrompt {
		start = s.PromptMs
	}
	end := s.LastMs
	if s.HasStop {
		end = s.StopMs
	}
	if start == 0 || end < start {
		return 0
	}
	return float64(end-start) / 1000
}

func (s *storyAccumulator) activeSeconds() float64 {
	if len(s.Intervals) == 0 {
		return 0
	}
	sort.Slice(s.Intervals, func(i, j int) bool {
		if s.Intervals[i].start == s.Intervals[j].start {
			return s.Intervals[i].end < s.Intervals[j].end
		}
		return s.Intervals[i].start < s.Intervals[j].start
	})
	total := int64(0)
	current := s.Intervals[0]
	for _, interval := range s.Intervals[1:] {
		if interval.start <= current.end {
			if interval.end > current.end {
				current.end = interval.end
			}
			continue
		}
		total += current.end - current.start
		current = interval
	}
	total += current.end - current.start
	if total < 0 {
		return 0
	}
	return float64(total) / 1000
}

func (s *storyAccumulator) outcome() string {
	if s.BlockedReason != "" {
		return "blocked"
	}
	if s.HasStop {
		return "completed"
	}
	return "open"
}

func storyToolKey(sessionID, toolCallID string) string {
	return sessionID + "::" + toolCallID
}

func payloadString(payload map[string]any, key string) string {
	if len(payload) == 0 {
		return ""
	}
	if value, ok := payload[key]; ok {
		if text, ok := value.(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

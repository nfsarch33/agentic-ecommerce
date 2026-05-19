// File scope: v3.9.0 EC-5-5 content performance feedback loop.
//
// Maintains an exponential moving average (EMA) of engagement
// scores per (tenant, content_id, channel). The smoothed score
// drives:
//   - The hashtag agent's BiasProvider port (EC-5-4 reads back to
//     decide longer captions / different hashtag count when the
//     EMA shows recent wins on that bias).
//   - The Prometheus gauge ec_content_ema_score{tenant_id, channel,
//     content_type} so the dashboard can pivot per channel.
//   - The eventbus ContentEMAUpdatedEvent so downstream consumers
//     (alerting, content scheduler) can react.
//
// EMA formula:
//
//	ema_n = alpha * x_n + (1 - alpha) * ema_{n-1}
//
// Where alpha is the smoothing coefficient (default 0.2 per the
// plan). Higher alpha means more weight on recent samples.
//
// Reuse evidence:
//   - eventbus.Publisher contract from v3.3.0 EC-3-3.
//   - The state-machine + sentinel pattern mirrors the v3.5.0
//     EC-6-3 pricing agent.
//   - The metric port pattern mirrors v3.5.0 + v3.8.0 metrics
//     facades.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4 -- 16-sprint streak; v3.9.0 sprint 16 target):
//   - Update (envelope -> validate -> compute EMA -> store -> emit)
//   - validateMetric (pure)
//   - computeEMA (pure)
//   - storeRecord (synchronous map write)
//   - emitUpdate (eventbus dispatch)
//
// Each helper stays under cyclomatic 6.
package content

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

// DefaultEMAAlpha is the EC-5-5 default smoothing coefficient.
// Higher values weigh recent samples more.
const DefaultEMAAlpha = 0.2

// CaptionLengthLongThreshold is the proxy for "long caption"
// when computing bias feedback. >= 1500 runes is considered long.
const CaptionLengthLongThreshold = 1500

// EC-5-5 typed sentinels.
var (
	// ErrPerformanceLoopUnconfigured is returned when a required
	// dependency is missing.
	ErrPerformanceLoopUnconfigured = errors.New("performance_loop: unconfigured")

	// ErrPerformanceLoopClosed is returned by Update after Close.
	ErrPerformanceLoopClosed = errors.New("performance_loop: closed")

	// ErrInvalidEngagementMetric is returned when the supplied
	// metric is malformed (missing content_id / channel / negative
	// score etc).
	ErrInvalidEngagementMetric = errors.New("performance_loop: invalid engagement metric")

	// ErrInsufficientDataForEMA is the sentinel callers can use
	// when reading back the EMA before any samples have arrived.
	ErrInsufficientDataForEMA = errors.New("performance_loop: insufficient data for EMA")
)

// EngagementMetric is one engagement observation submitted to the
// loop. EngagementScore is the platform-normalised score in [0, 100]
// (likes + comments + shares + conversions weighted by the platform
// multiplier the upstream agent already computed).
type EngagementMetric struct {
	ContentID       string
	Channel         string
	ContentType     string
	EngagementScore float64 // [0, 100]
	CaptionLength   int     // runes, optional bias signal
	HashtagCount    int     // optional bias signal
	ObservedAt      time.Time
}

// EMARecord is the persisted EMA state per (tenant, content, channel).
type EMARecord struct {
	ContentID           string
	Channel             string
	ContentType         string
	EMAScore            float64
	LastEngagementScore float64
	SampleCount         int
	Alpha               float64
	LongCaptionWins     int // running count of high-EMA samples that had a long caption
	BiasHashtagSum      int
	UpdatedAt           time.Time
}

// PerformanceLoopMetrics is the small port the loop emits the gauge
// + counter through.
type PerformanceLoopMetrics interface {
	SetContentEMAScore(tenantID, channel, contentType string, score float64)
	RecordContentEMAUpdate(tenantID, channel string)
	ObserveContentEMAUpdateDuration(durationSec float64)
}

// PerformanceLoopKPISample is the v3.9.0 EvoMap KPI sample.
type PerformanceLoopKPISample struct {
	TenantID    string
	Channel     string
	ContentType string
	EMAScore    float64
	SampleCount int
}

// PerformanceLoopKPIHook is the optional EvoMap emission hook.
type PerformanceLoopKPIHook func(PerformanceLoopKPISample)

// PerformanceLoopConfig wires the loop.
type PerformanceLoopConfig struct {
	TenantID  string
	Publisher eventbus.Publisher
	Alpha     float64
	Metrics   PerformanceLoopMetrics
	KPIHook   PerformanceLoopKPIHook
	Now       func() time.Time
}

// PerformanceLoop is the v3.9.0 EC-5-5 EMA learner.
type PerformanceLoop struct {
	cfg    PerformanceLoopConfig
	logger *slog.Logger

	mu      sync.Mutex
	records map[string]EMARecord
	closed  bool
}

// NewPerformanceLoop constructs a loop.
func NewPerformanceLoop(logger *slog.Logger, cfg PerformanceLoopConfig) (*PerformanceLoop, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrPerformanceLoopUnconfigured)
	}
	if cfg.Publisher == nil {
		return nil, fmt.Errorf("%w: Publisher required", ErrPerformanceLoopUnconfigured)
	}
	if cfg.Alpha <= 0 || cfg.Alpha > 1 {
		cfg.Alpha = DefaultEMAAlpha
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &PerformanceLoop{
		cfg:     cfg,
		logger:  logger,
		records: map[string]EMARecord{},
	}, nil
}

// Close marks the loop closed. Implements lifecycle.Closer.
func (l *PerformanceLoop) Close(_ context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	return nil
}

// Update folds an engagement metric into the EMA. Cyclomatic 5.
func (l *PerformanceLoop) Update(ctx context.Context, m EngagementMetric) (EMARecord, error) {
	if err := l.guard(); err != nil {
		return EMARecord{}, err
	}
	if err := validateMetric(m); err != nil {
		return EMARecord{}, err
	}
	start := l.cfg.Now()
	rec := l.applyUpdate(m, start)
	l.emitUpdate(ctx, rec)
	l.recordMetric(rec)
	l.recordKPI(rec)
	if l.cfg.Metrics != nil {
		l.cfg.Metrics.ObserveContentEMAUpdateDuration(l.cfg.Now().Sub(start).Seconds())
	}
	return rec, nil
}

func (l *PerformanceLoop) applyUpdate(m EngagementMetric, now time.Time) EMARecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := buildEMAKey(m.ContentID, m.Channel)
	rec, ok := l.records[key]
	if !ok {
		rec = EMARecord{
			ContentID:   m.ContentID,
			Channel:     m.Channel,
			ContentType: m.ContentType,
			Alpha:       l.cfg.Alpha,
		}
	}
	rec.EMAScore = computeEMA(rec.EMAScore, m.EngagementScore, l.cfg.Alpha)
	rec.LastEngagementScore = m.EngagementScore
	rec.SampleCount++
	rec.UpdatedAt = now
	if rec.EMAScore >= 50 && m.CaptionLength >= CaptionLengthLongThreshold {
		rec.LongCaptionWins++
	}
	if m.HashtagCount > 0 {
		rec.BiasHashtagSum += m.HashtagCount
	}
	l.records[key] = rec
	return rec
}

// computeEMA is the pure EMA function. Cyclomatic 2.
func computeEMA(prev, sample, alpha float64) float64 {
	return alpha*sample + (1-alpha)*prev
}

// Lookup returns the EMA record for (content_id, channel). The
// boolean is false when no record exists.
func (l *PerformanceLoop) Lookup(contentID, channel string) (EMARecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.records[buildEMAKey(contentID, channel)]
	return rec, ok
}

// MaxScoreForChannel returns the highest EMA across all content on
// the channel, or 0 if no records exist.
func (l *PerformanceLoop) MaxScoreForChannel(channel string) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	var max float64
	for _, rec := range l.records {
		if rec.Channel != channel {
			continue
		}
		if rec.EMAScore > max {
			max = rec.EMAScore
		}
	}
	return max
}

// BiasFor implements the EMABiasProvider port consumed by the
// EC-5-4 hashtag agent. Returns PreferLongerCaption=true when at
// least 50% of the channel's high-EMA samples used a long caption.
func (l *PerformanceLoop) BiasFor(channel, contentType string) (HashtagBias, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var (
		seen           int
		longWins       int
		hashtagSum     int
		hashtagSamples int
		bestEMA        float64
	)
	for _, rec := range l.records {
		if rec.Channel != channel {
			continue
		}
		if contentType != "" && rec.ContentType != contentType {
			continue
		}
		seen++
		longWins += rec.LongCaptionWins
		if rec.SampleCount > 0 && rec.BiasHashtagSum > 0 {
			hashtagSum += rec.BiasHashtagSum
			hashtagSamples += rec.SampleCount
		}
		if rec.EMAScore > bestEMA {
			bestEMA = rec.EMAScore
		}
	}
	if seen == 0 {
		return HashtagBias{}, false
	}
	bias := HashtagBias{
		PreferLongerCaption: longWins >= seen/2 && longWins > 0,
		EMAScore:            bestEMA,
	}
	if hashtagSamples > 0 {
		bias.BiasHashtagCount = hashtagSum / hashtagSamples
	}
	return bias, true
}

func (l *PerformanceLoop) emitUpdate(ctx context.Context, rec EMARecord) {
	payload := eventbus.ContentEMAUpdatedPayload{
		Version:             eventbus.ContentEMAUpdatedPayloadVersion,
		TenantID:            l.cfg.TenantID,
		ContentID:           rec.ContentID,
		Channel:             rec.Channel,
		ContentType:         rec.ContentType,
		EMAScore:            rec.EMAScore,
		LastEngagementScore: rec.LastEngagementScore,
		SampleCount:         rec.SampleCount,
		Alpha:               rec.Alpha,
		OccurredAt:          rec.UpdatedAt,
	}
	evt, err := eventbus.NewContentEMAUpdatedEvent("agent.content.performance_loop", rec.UpdatedAt, payload)
	if err != nil {
		l.logger.Error("performance_loop.event_invalid", "tenant_id", l.cfg.TenantID, "error", err)
		return
	}
	if err := l.cfg.Publisher.Publish(ctx, evt); err != nil {
		l.logger.Error("performance_loop.publish_failed", "tenant_id", l.cfg.TenantID, "error", err)
	}
}

func (l *PerformanceLoop) recordMetric(rec EMARecord) {
	if l.cfg.Metrics == nil {
		return
	}
	l.cfg.Metrics.SetContentEMAScore(l.cfg.TenantID, rec.Channel, rec.ContentType, rec.EMAScore)
	l.cfg.Metrics.RecordContentEMAUpdate(l.cfg.TenantID, rec.Channel)
}

func (l *PerformanceLoop) recordKPI(rec EMARecord) {
	if l.cfg.KPIHook == nil {
		return
	}
	l.cfg.KPIHook(PerformanceLoopKPISample{
		TenantID:    l.cfg.TenantID,
		Channel:     rec.Channel,
		ContentType: rec.ContentType,
		EMAScore:    rec.EMAScore,
		SampleCount: rec.SampleCount,
	})
}

func (l *PerformanceLoop) guard() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrPerformanceLoopClosed
	}
	return nil
}

func validateMetric(m EngagementMetric) error {
	if strings.TrimSpace(m.ContentID) == "" {
		return fmt.Errorf("%w: content_id required", ErrInvalidEngagementMetric)
	}
	if strings.TrimSpace(m.Channel) == "" {
		return fmt.Errorf("%w: channel required", ErrInvalidEngagementMetric)
	}
	if m.EngagementScore < 0 {
		return fmt.Errorf("%w: engagement_score cannot be negative", ErrInvalidEngagementMetric)
	}
	return nil
}

func buildEMAKey(contentID, channel string) string {
	return contentID + "\x00" + channel
}

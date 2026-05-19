// File scope: v3.9.0 EC-5-5 content performance feedback loop RED tests.
package content

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

type stubLoopPublisher struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (p *stubLoopPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *stubLoopPublisher) Close() error { return nil }

func newPerformanceLoopHarness(t *testing.T, opts ...func(*PerformanceLoopConfig)) (*PerformanceLoop, *stubLoopPublisher) {
	t.Helper()
	pub := &stubLoopPublisher{}
	clk := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	cfg := PerformanceLoopConfig{
		TenantID:  "tenant-1",
		Publisher: pub,
		Now:       func() time.Time { return clk },
		Alpha:     0.2,
	}
	for _, o := range opts {
		o(&cfg)
	}
	loop, err := NewPerformanceLoop(nil, cfg)
	if err != nil {
		t.Fatalf("NewPerformanceLoop: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close(context.Background()) })
	return loop, pub
}

func TestPerformanceLoop_EMASmoothing(t *testing.T) {
	t.Parallel()
	loop, _ := newPerformanceLoopHarness(t)
	const contentID = "post-1"
	scores := []float64{60, 70, 65, 80}
	var last float64
	for _, s := range scores {
		got, err := loop.Update(context.Background(), EngagementMetric{
			ContentID:       contentID,
			Channel:         "tiktok",
			ContentType:     "video",
			EngagementScore: s,
		})
		if err != nil {
			t.Fatalf("Update %f: %v", s, err)
		}
		last = got.EMAScore
	}
	// alpha=0.2 over [60,70,65,80] starting from 0: 12, 23.6, 31.88, 41.504
	expected := 41.504
	if math.Abs(last-expected) > 0.01 {
		t.Fatalf("expected EMA %.3f, got %.3f", expected, last)
	}
}

func TestPerformanceLoop_EMACoefficientCustomizable(t *testing.T) {
	t.Parallel()
	loopA, _ := newPerformanceLoopHarness(t, func(cfg *PerformanceLoopConfig) {
		cfg.Alpha = 0.5
	})
	loopB, _ := newPerformanceLoopHarness(t)
	const contentID = "post-1"
	for _, s := range []float64{60, 70, 65, 80} {
		_, _ = loopA.Update(context.Background(), EngagementMetric{
			ContentID: contentID, Channel: "tiktok", ContentType: "video", EngagementScore: s,
		})
		_, _ = loopB.Update(context.Background(), EngagementMetric{
			ContentID: contentID, Channel: "tiktok", ContentType: "video", EngagementScore: s,
		})
	}
	a, okA := loopA.Lookup(contentID, "tiktok")
	b, okB := loopB.Lookup(contentID, "tiktok")
	if !okA || !okB {
		t.Fatalf("expected both EMAs to exist")
	}
	if a.EMAScore == b.EMAScore {
		t.Fatalf("alpha 0.5 vs 0.2 should differ; got both=%.3f", a.EMAScore)
	}
}

func TestPerformanceLoop_PerPlatformIsolation(t *testing.T) {
	t.Parallel()
	loop, _ := newPerformanceLoopHarness(t)
	const contentID = "post-1"
	for _, s := range []float64{50, 60, 70} {
		_, _ = loop.Update(context.Background(), EngagementMetric{
			ContentID: contentID, Channel: "tiktok", ContentType: "video", EngagementScore: s,
		})
	}
	for _, s := range []float64{20, 25, 22} {
		_, _ = loop.Update(context.Background(), EngagementMetric{
			ContentID: contentID, Channel: "facebook", ContentType: "post", EngagementScore: s,
		})
	}
	tt, _ := loop.Lookup(contentID, "tiktok")
	fb, _ := loop.Lookup(contentID, "facebook")
	if tt.EMAScore <= fb.EMAScore {
		t.Fatalf("tiktok %.3f should be > facebook %.3f", tt.EMAScore, fb.EMAScore)
	}
}

func TestPerformanceLoop_FeedsBackToHashtagAgent(t *testing.T) {
	t.Parallel()
	loop, _ := newPerformanceLoopHarness(t)
	for _, s := range []float64{75, 80, 85, 90, 95} {
		_, _ = loop.Update(context.Background(), EngagementMetric{
			ContentID:       "p-1",
			Channel:         "tiktok",
			ContentType:     "video",
			EngagementScore: s,
			CaptionLength:   2000, // long captions outperformed
		})
	}
	// BiasFor should now report PreferLongerCaption=true on tiktok
	bias, ok := loop.BiasFor("tiktok", "video")
	if !ok {
		t.Fatalf("expected bias to be available after several updates")
	}
	if !bias.PreferLongerCaption {
		t.Fatalf("expected PreferLongerCaption true after long-caption series, got %+v", bias)
	}
}

func TestPerformanceLoop_HandlesMissingMetrics(t *testing.T) {
	t.Parallel()
	loop, _ := newPerformanceLoopHarness(t)
	_, err := loop.Update(context.Background(), EngagementMetric{
		ContentID: "", Channel: "tiktok", ContentType: "video", EngagementScore: 50,
	})
	if !errors.Is(err, ErrInvalidEngagementMetric) {
		t.Fatalf("expected ErrInvalidEngagementMetric for missing content_id, got %v", err)
	}
	_, err = loop.Update(context.Background(), EngagementMetric{
		ContentID: "p", Channel: "tiktok", ContentType: "video", EngagementScore: -5,
	})
	if !errors.Is(err, ErrInvalidEngagementMetric) {
		t.Fatalf("expected ErrInvalidEngagementMetric for negative score, got %v", err)
	}
	_, err = loop.Update(context.Background(), EngagementMetric{
		ContentID: "p", Channel: "", ContentType: "video", EngagementScore: 50,
	})
	if !errors.Is(err, ErrInvalidEngagementMetric) {
		t.Fatalf("expected ErrInvalidEngagementMetric for missing channel, got %v", err)
	}
}

func TestPerformanceLoop_EmitsEMAUpdatedEvent(t *testing.T) {
	t.Parallel()
	loop, pub := newPerformanceLoopHarness(t)
	_, err := loop.Update(context.Background(), EngagementMetric{
		ContentID: "p-1", Channel: "tiktok", ContentType: "video", EngagementScore: 50,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	pub.mu.Lock()
	count := len(pub.events)
	if count == 0 || pub.events[0].Type != eventbus.ContentEMAUpdated {
		pub.mu.Unlock()
		t.Fatalf("expected ContentEMAUpdated event, got count=%d events=%v", count, pub.events)
	}
	pub.mu.Unlock()
}

func TestPerformanceLoop_MaxScorePerChannel(t *testing.T) {
	t.Parallel()
	loop, _ := newPerformanceLoopHarness(t)
	// Two contents on tiktok; one higher EMA than the other.
	for _, s := range []float64{50, 50, 50} {
		_, _ = loop.Update(context.Background(), EngagementMetric{
			ContentID: "p-1", Channel: "tiktok", ContentType: "video", EngagementScore: s,
		})
	}
	for _, s := range []float64{90, 90, 90} {
		_, _ = loop.Update(context.Background(), EngagementMetric{
			ContentID: "p-2", Channel: "tiktok", ContentType: "video", EngagementScore: s,
		})
	}
	maxScore := loop.MaxScoreForChannel("tiktok")
	if maxScore <= 0 {
		t.Fatalf("expected positive max score, got %.3f", maxScore)
	}
}

func TestPerformanceLoop_ClosedReturnsError(t *testing.T) {
	t.Parallel()
	loop, _ := newPerformanceLoopHarness(t)
	if err := loop.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := loop.Update(context.Background(), EngagementMetric{
		ContentID: "p-1", Channel: "tiktok", ContentType: "video", EngagementScore: 50,
	})
	if !errors.Is(err, ErrPerformanceLoopClosed) {
		t.Fatalf("expected ErrPerformanceLoopClosed, got %v", err)
	}
}

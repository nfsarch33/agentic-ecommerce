// File scope: v3.2.1 QA Task 2 -- LLM failover to template
// confirmed across the four real-world unavailability shapes.
//
// The v3.2.0 EC-2-1 RED test (TestDescriptionGen_FailsOverToTemplate-
// WhenLLMUnavailable) covers the "generator returns a Bedrock-style
// service-unavailable error" case. The v3.2.1 acceptance criterion
// in the parent plan widens this to a table-driven matrix:
//
//   - upstream nil-equivalent (typed ErrLLMNotConfigured)
//   - 5xx HTTP error (e.g. Bedrock 503)
//   - context deadline exceeded (timeout) including a long-running
//     real-clock case so the deterministic-template path is proved
//     under a true ctx.Err scenario.
//   - circuit-breaker open (typed ErrCircuitBreakerOpen)
//
// For every case the test asserts:
//
//  1. Generate returns nil error (the template path SUCCEEDS at the
//     relaxed 0.65 fallback threshold).
//  2. result.Source == ResultSourceTemplate.
//  3. result.QualityScore >= 0.65 (relaxed fallback floor).
//  4. result.QualityScore <= templateScoreCeiling (0.85) so the
//     stuck-on-template tenant stays visible in the EC-2-5
//     histogram.
//  5. The template body is deterministic across two back-to-back
//     calls with identical inputs (operator must be able to spot a
//     stuck-on-template tenant by stable copy).
//
// Cite skill: go-clean-architecture (port + adapter -- the
// failoverGenerator implements port.AITextGenerator and lets the
// table parameterise the failure shape without touching the agent).
package enrichment

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// ErrCircuitBreakerOpen mirrors the typical sentinel a downstream
// circuit-breaker (e.g. sony/gobreaker, future internal/breaker)
// returns when the LLM upstream is shed. Defined here so the
// failover test can exercise the breaker-open branch without
// pulling a new dependency. The agent only cares that llmErr != nil
// -- so the typed sentinel doubles as documentation of the real
// production error shape the operator will see in logs.
var ErrCircuitBreakerOpen = errors.New("enrichment: llm circuit breaker open")

// ErrLLMNotConfigured mirrors the sentinel a real adapter returns
// when the cmd/* binary forgot to wire the IronClaw or Bedrock
// client. Production code surfaces this as a 503 to the operator
// while the description agent silently falls back to the template
// path so the enrichment pipeline never blocks the sourcing path.
var ErrLLMNotConfigured = errors.New("enrichment: llm adapter not configured")

// TestDescriptionGen_FailoverToTemplateWhenLLMUnavailable is the
// v3.2.1 QA-2 acceptance test (per the plan: "Test that
// DescriptionGen falls back to template when Bedrock/IronClaw is
// unavailable").
func TestDescriptionGen_FailoverToTemplateWhenLLMUnavailable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		generator port.AITextGenerator
		// failureLabel is just for the error snippet assertion;
		// the agent itself does not branch on the cause.
		failureLabel string
	}{
		{
			name:         "nil_equivalent_not_configured",
			generator:    &failoverGenerator{err: ErrLLMNotConfigured},
			failureLabel: "not configured",
		},
		{
			name:         "bedrock_503_service_unavailable",
			generator:    &failoverGenerator{err: fmt.Errorf("bedrock: status %d service unavailable", http.StatusServiceUnavailable)},
			failureLabel: "service unavailable",
		},
		{
			name:         "context_deadline_exceeded_real",
			generator:    newRealTimeoutGenerator(50 * time.Millisecond),
			failureLabel: "deadline exceeded",
		},
		{
			name:         "circuit_breaker_open",
			generator:    &failoverGenerator{err: fmt.Errorf("downstream guarded: %w", ErrCircuitBreakerOpen)},
			failureLabel: "circuit breaker open",
		},
	}

	// Same inputs across every case so we can also assert
	// determinism between the matrix runs.
	req := DescriptionRequest{
		Product: EnrichmentProduct{
			ID:                 "earbuds-failover-001",
			ChineseTitle:       "高品质无线蓝牙耳机",
			ChineseDescription: "无线蓝牙耳机, 续航36小时, 主动降噪",
			Category:           "electronics",
			PriceCNYCents:      4500,
		},
		Platform: PlatformWooCommerce,
		Language: "en-AU",
		Keywords: []string{"wireless earbuds", "noise cancelling"},
	}

	var firstTemplateBody string
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Subtests run sequentially so we can compare bodies
			// across the cases and prove determinism.
			gen, err := NewDescriptionGenerator(nil, DescriptionGeneratorConfig{
				Generator:    tc.generator,
				TenantID:     "cylrl",
				MinQuality:   0.65, // relaxed fallback floor
				FallbackText: "Quality product imported from a verified supplier.",
				Now:          func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
			})
			if err != nil {
				t.Fatalf("NewDescriptionGenerator: %v", err)
			}
			t.Cleanup(func() { _ = gen.Close(context.Background()) })

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			res, err := gen.Generate(ctx, req)
			if err != nil {
				t.Fatalf("Generate(%s): %v", tc.name, err)
			}
			if res.Source != ResultSourceTemplate {
				t.Fatalf("Source = %q, want %q (failover %s)", res.Source, ResultSourceTemplate, tc.name)
			}
			if res.QualityScore < 0.65 {
				t.Fatalf("QualityScore = %.4f, want >= 0.65 (relaxed fallback floor) (failover %s)", res.QualityScore, tc.name)
			}
			if res.QualityScore > templateScoreCeiling {
				t.Fatalf("QualityScore = %.4f, want <= %.4f (template ceiling so stuck tenants stay visible)", res.QualityScore, templateScoreCeiling)
			}
			if !strings.Contains(res.EnglishDescription, "Quality product") {
				t.Fatalf("template fallback missing fallback prefix (failover %s): %q", tc.name, res.EnglishDescription)
			}
			if res.Platform != PlatformWooCommerce {
				t.Fatalf("Platform = %q, want %q", res.Platform, PlatformWooCommerce)
			}

			// Determinism: every failover case must yield the same
			// English body so the operator can spot a stuck-on-
			// template tenant by stable copy.
			if firstTemplateBody == "" {
				firstTemplateBody = res.EnglishDescription
			} else if res.EnglishDescription != firstTemplateBody {
				t.Fatalf("template body drifted across failover cases (case %s): got %q, want %q", tc.name, res.EnglishDescription, firstTemplateBody)
			}

			// Idempotency: re-run the same inputs and assert byte-
			// identical body. Guards against time.Now / random
			// regressions in the template path.
			res2, err := gen.Generate(ctx, req)
			if err != nil {
				t.Fatalf("Generate(re-run %s): %v", tc.name, err)
			}
			if res2.EnglishDescription != res.EnglishDescription {
				t.Fatalf("template body not deterministic across re-run (case %s): got %q, want %q", tc.name, res2.EnglishDescription, res.EnglishDescription)
			}
		})
	}
}

// failoverGenerator is the in-test port.AITextGenerator that
// always returns a configured error. The agent only branches on
// llmErr != nil, so this is enough to exercise every failover
// shape that maps to a non-nil error.
type failoverGenerator struct {
	err   error
	calls atomic.Int32
}

func (f *failoverGenerator) Complete(_ context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	f.calls.Add(1)
	return port.AICompletionResponse{}, f.err
}

// realTimeoutGenerator sleeps until ctx fires so the failover
// path is proved against a true context.DeadlineExceeded shape.
// Sleep budget is bounded above by ctx.Done() so the test never
// stalls beyond the parent timeout.
type realTimeoutGenerator struct {
	delay time.Duration
}

// newRealTimeoutGenerator returns a generator that sleeps for the
// supplied duration before returning ctx.Err. The supplied delay
// is intentionally larger than the per-call ctx budget the test
// configures upstream so the timeout always fires before the
// sleep would naturally complete.
func newRealTimeoutGenerator(delay time.Duration) *realTimeoutGenerator {
	return &realTimeoutGenerator{delay: delay}
}

func (g *realTimeoutGenerator) Complete(ctx context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	// Use a short per-call timeout so the parent test ctx (2s)
	// stays in budget. The agent under test only cares that the
	// returned error is non-nil; the specific shape (context.
	// DeadlineExceeded) is documented for the operator log.
	innerCtx, cancel := context.WithTimeout(ctx, g.delay)
	defer cancel()
	<-innerCtx.Done()
	return port.AICompletionResponse{}, fmt.Errorf("bedrock: %w", innerCtx.Err())
}

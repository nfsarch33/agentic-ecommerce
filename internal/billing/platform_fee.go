// File scope: v3.5.0 EC-6-2 platform fee + AUD/CNY FX calculator.
//
// EC-6-2 is the foundation story for v3.5.0 Epic 6 (pricing). Pure
// in-memory logic with no external IO except the pluggable
// FXRateProvider port. The v3.1.0 China sourcing pipeline produces
// CNY-denominated cost figures; the v2.5.0 Stripe billing module
// owns Stripe webhook plumbing; this calculator bridges them so the
// EC-6-3 dynamic pricing agent can reason about gross margin in AUD.
//
// Reuse evidence:
//   - The `_AUDCents` integer arithmetic mirrors the v2.5.0
//     internal/billing.Invoice.AmountCents pattern.
//   - The typed-error sentinels (ErrFXRateStale, ErrUnsupportedChannel,
//     ErrInvalidPriceComponents) follow the v3.4.0 EC-4-3
//     channel.router.go ErrChannel* sentinel pattern.
//   - The PlatformFeeCalculator port + adapter shape mirrors the
//     v2.5.0 internal/billing.PlanCatalog port pattern.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 7-sprint streak; v3.5.0 sprint 8 target):
//   - validatePriceComponents (pure; returns ErrInvalidPriceComponents)
//   - resolveFX (provider call + staleness check)
//   - computePlatformFee (channel dispatch via fee handler map)
//   - assembleMargin (pure shape construction)
//
// Each helper stays under cyclomatic 6. CalculateMargin becomes a
// linear pipeline of these helpers with no nested branching.
//
// Cite skill: go-clean-architecture (port + adapter; the calculator
// depends on FXRateProvider not on a concrete HTTP client).
package billing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Channel identifies a sales channel for fee calculation. Mirrors
// the values used by the v3.4.0 EC-4-3 channel router so dashboards
// can pivot on a single label namespace.
type Channel string

// Channel constants. RedNote is intentionally listed but unsupported
// (the v3.4.0 EC-4-1 RedNote integration is uiauto-only; commission
// integration is deferred to a future operator-set surface). Treat
// it as N/A for v3.5.0 fee calc.
const (
	ChannelTikTok      Channel = "tiktok"
	ChannelFacebook    Channel = "facebook"
	ChannelWooCommerce Channel = "woocommerce"
	ChannelRedNote     Channel = "rednote"
)

// DefaultTikTokCommissionPct is the v3.5.0 default 5% commission.
// Per the EC-6-2 spec: TikTok Shop commission is variable in the
// 2-5% band; operator config sets the per-product override.
const DefaultTikTokCommissionPct = 0.05

// MinTikTokCommissionPct caps the operator-configurable lower
// bound (2%). Values below the floor are clamped UP to the floor.
const MinTikTokCommissionPct = 0.02

// MaxTikTokCommissionPct caps the upper bound (5%). Values above
// the ceiling are clamped DOWN to the ceiling. Operator typos that
// pass 50% (instead of 0.05) clamp safely.
const MaxTikTokCommissionPct = 0.05

// FacebookCommissionPct is the EC-6-2 flat commission for Facebook
// Shop -- documented at 5% by Meta Commerce Manager.
const FacebookCommissionPct = 0.05

// StripePctRate is the Stripe-on-WooCommerce variable rate (1.75%).
// Per the EC-6-2 spec referencing AU Stripe pricing.
const StripePctRate = 0.0175

// StripeFixedAUDCents is the per-transaction fixed Stripe fee
// (A$0.30 per the EC-6-2 spec).
const StripeFixedAUDCents = 30

// DefaultFXStaleness is the ceiling above which the calculator
// surfaces ErrFXRateStale. 24 hours per the EC-6-2 acceptance
// criterion.
const DefaultFXStaleness = 24 * time.Hour

// EC-6-2 typed sentinels.
var (
	// ErrFXRateStale is returned when the resolved FX rate is older
	// than the configured MaxFXAge (default 24h). Wraps the rate
	// age + ceiling so the operator dashboard can render the gap.
	ErrFXRateStale = errors.New("billing: fx rate stale")

	// ErrUnsupportedChannel is returned when a channel does not
	// have a registered fee model (e.g. RedNote in v3.5.0).
	ErrUnsupportedChannel = errors.New("billing: channel unsupported for fee calc")

	// ErrInvalidPriceComponents is returned when PriceComponents
	// fails the validate gate (zero/negative selling price,
	// negative cost, etc).
	//
	// Note (v6.1.0 CF-17): FX provider misbehaviour (rate <= 0)
	// is NO LONGER routed through this sentinel; callers expecting
	// to distinguish caller-input errors from provider pollution
	// MUST check ErrInvalidFXRate for the latter.
	ErrInvalidPriceComponents = errors.New("billing: invalid price components")

	// ErrInvalidFXRate is returned when the FX provider yields a
	// non-positive (AUDPerCNY <= 0) rate. Provider-side pollution
	// is operationally distinct from caller-side input errors
	// (ErrInvalidPriceComponents), so v6.1.0 carry-forward CF-17
	// pulled it out of the broader sentinel into its own typed
	// surface. Adapters write the same sentinel.
	ErrInvalidFXRate = errors.New("billing: invalid fx rate")

	// ErrFXRateUnconfigured is returned by NewPlatformFeeCalculator
	// when the FXRateProvider port is nil.
	ErrFXRateUnconfigured = errors.New("billing: fx rate provider unconfigured")
)

// FXRate is a typed AUD/CNY exchange rate sample. AUDPerCNY is the
// multiplier such that `cost_aud = cost_cny * AUDPerCNY`. Source
// captures the provider name (e.g. "rba", "fawazahmed0", "static")
// for diagnostics.
type FXRate struct {
	AUDPerCNY float64
	FetchedAt time.Time
	Source    string
}

// IsStale returns true when the rate is older than max (or the
// default 24h when max <= 0). A zero FetchedAt is always treated
// as stale (defensive default for adapters that forgot to stamp).
func (r FXRate) IsStale(now time.Time, max time.Duration) bool {
	if max <= 0 {
		max = DefaultFXStaleness
	}
	if r.FetchedAt.IsZero() {
		return true
	}
	return now.Sub(r.FetchedAt) > max
}

// AgeSeconds returns the rate's age in seconds at the supplied
// reference time. Zero FetchedAt -> +Inf so the dashboard can render
// "unknown age".
func (r FXRate) AgeSeconds(now time.Time) float64 {
	if r.FetchedAt.IsZero() {
		return math.Inf(1)
	}
	age := now.Sub(r.FetchedAt).Seconds()
	if age < 0 {
		return 0
	}
	return age
}

// FXRateProvider is the small port the calculator consumes. Two
// adapters ship in v3.5.0:
//
//   - StaticFXRateProvider: in-memory test fixture.
//   - FXRateFileCacheProvider: file-backed JSON cache (live API
//     integration deferred to v3.5.1 -- the operator runs a daily
//     refresh script that writes the cache file).
type FXRateProvider interface {
	// LatestRate returns the latest cached rate. Implementations
	// MUST stamp FetchedAt; the calculator branches on it for
	// staleness detection. A returned error is wrapped + surfaced
	// to the caller without translation.
	LatestRate(ctx context.Context) (FXRate, error)
}

// PriceComponents is the input shape for CalculateMargin /
// PlatformFee. All currency fields are integer cents to keep the
// arithmetic deterministic.
type PriceComponents struct {
	// Channel is the destination channel. ErrUnsupportedChannel
	// when the value is not one of the known constants.
	Channel Channel
	// SellingPriceAUDCents is the storefront price in AUD cents.
	SellingPriceAUDCents int
	// CostCNYCents is the supplier cost (1688/Taobao/etc) in CNY
	// cents. Zero is allowed (sample tests); negative is rejected.
	CostCNYCents int
	// ShippingEstAUDCents is the operator-supplied shipping
	// estimate in AUD cents. Zero is allowed; negative rejected.
	ShippingEstAUDCents int
	// TikTokCommissionPct is the operator-overridable per-product
	// commission rate for the TikTok channel. Zero means "use the
	// default 5%". Values outside the [2%, 5%] band are clamped.
	// Ignored on non-TikTok channels.
	TikTokCommissionPct float64
}

// MarginResult breaks down each component so the EC-6-3 dynamic
// pricing agent + the v3.9.0 EC-6-5 margin dashboard can render
// without a second pass through the calculator.
type MarginResult struct {
	Channel              Channel
	SellingPriceAUDCents int
	CostAUDCents         int
	PlatformFeeAUDCents  int
	ShippingEstAUDCents  int
	NetAUDCents          int
	GrossMarginPct       float64
	FXRate               FXRate
}

// PlatformFeeCalculatorConfig wires a calculator. FXProvider is
// REQUIRED. MaxFXAge defaults to 24h. Now defaults to time.Now().UTC.
type PlatformFeeCalculatorConfig struct {
	FXProvider FXRateProvider
	MaxFXAge   time.Duration
	Now        func() time.Time
}

// PlatformFeeCalculator is the v3.5.0 EC-6-2 calculator.
type PlatformFeeCalculator struct {
	fxProvider FXRateProvider
	maxAge     time.Duration
	now        func() time.Time
}

// NewPlatformFeeCalculator constructs a calculator. Returns
// ErrFXRateUnconfigured when FXProvider is nil.
func NewPlatformFeeCalculator(cfg PlatformFeeCalculatorConfig) (*PlatformFeeCalculator, error) {
	if cfg.FXProvider == nil {
		return nil, fmt.Errorf("%w: FXProvider required", ErrFXRateUnconfigured)
	}
	if cfg.MaxFXAge <= 0 {
		cfg.MaxFXAge = DefaultFXStaleness
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &PlatformFeeCalculator{
		fxProvider: cfg.FXProvider,
		maxAge:     cfg.MaxFXAge,
		now:        cfg.Now,
	}, nil
}

// MaxFXAge returns the configured staleness ceiling. Useful for
// dashboards + the EC-9-5 operator alert centre.
func (c *PlatformFeeCalculator) MaxFXAge() time.Duration { return c.maxAge }

// PlatformFee returns the platform fee component (in AUD cents)
// for a single transaction. Validation + per-channel dispatch
// live in helpers so this method body stays cyclomatic 1.
func (c *PlatformFeeCalculator) PlatformFee(p PriceComponents) (int, error) {
	if err := validatePriceComponents(p); err != nil {
		return 0, err
	}
	return computePlatformFee(p)
}

// CalculateMargin runs the full v3.5.0 EC-6-2 pipeline:
// validate -> resolveFX -> per-channel fee -> assembleMargin.
//
// Decomposition keeps the cyclomatic per-function under 6.
func (c *PlatformFeeCalculator) CalculateMargin(ctx context.Context, p PriceComponents) (MarginResult, error) {
	if err := validatePriceComponents(p); err != nil {
		return MarginResult{}, err
	}
	rate, err := c.resolveFX(ctx)
	if err != nil {
		return MarginResult{}, err
	}
	fee, err := computePlatformFee(p)
	if err != nil {
		return MarginResult{}, err
	}
	costAUD := convertCNYToAUDCents(p.CostCNYCents, rate.AUDPerCNY)
	return assembleMargin(p, rate, costAUD, fee), nil
}

// resolveFX fetches the latest rate and surfaces ErrFXRateStale
// when older than MaxFXAge. Provider errors pass through unchanged.
func (c *PlatformFeeCalculator) resolveFX(ctx context.Context) (FXRate, error) {
	rate, err := c.fxProvider.LatestRate(ctx)
	if err != nil {
		return FXRate{}, fmt.Errorf("billing: fx provider: %w", err)
	}
	if rate.AUDPerCNY <= 0 {
		return rate, fmt.Errorf("%w: fx rate non-positive (%.6f)", ErrInvalidFXRate, rate.AUDPerCNY)
	}
	if rate.IsStale(c.now(), c.maxAge) {
		return rate, fmt.Errorf("%w: age %.0fs > %.0fs", ErrFXRateStale, rate.AgeSeconds(c.now()), c.maxAge.Seconds())
	}
	return rate, nil
}

// validatePriceComponents enforces the EC-6-2 input contract.
// Pure; cyclomatic stays at 5 (one branch per invariant).
func validatePriceComponents(p PriceComponents) error {
	if strings.TrimSpace(string(p.Channel)) == "" {
		return fmt.Errorf("%w: channel required", ErrInvalidPriceComponents)
	}
	if p.SellingPriceAUDCents <= 0 {
		return fmt.Errorf("%w: selling_price_aud_cents must be > 0", ErrInvalidPriceComponents)
	}
	if p.CostCNYCents < 0 {
		return fmt.Errorf("%w: cost_cny_cents cannot be negative", ErrInvalidPriceComponents)
	}
	if p.ShippingEstAUDCents < 0 {
		return fmt.Errorf("%w: shipping_est_aud_cents cannot be negative", ErrInvalidPriceComponents)
	}
	return nil
}

// channelFeeFn is the per-channel fee handler signature. Returning
// (cents, error) lets RedNote return ErrUnsupportedChannel without
// a separate switch in the dispatcher.
type channelFeeFn func(p PriceComponents) (int, error)

// channelFeeRegistry is the dispatch table consulted by
// computePlatformFee. Keeping it package-level (not a method
// parameter) keeps the dispatcher cyclomatic at 1 (just a map
// lookup + nil check).
var channelFeeRegistry = map[Channel]channelFeeFn{
	ChannelTikTok:      feeForTikTok,
	ChannelFacebook:    feeForFacebook,
	ChannelWooCommerce: feeForWooCommerce,
	ChannelRedNote:     feeForRedNote,
}

// computePlatformFee dispatches to the per-channel handler.
// Cyclomatic 2 (lookup + ok branch).
func computePlatformFee(p PriceComponents) (int, error) {
	fn, ok := channelFeeRegistry[p.Channel]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrUnsupportedChannel, p.Channel)
	}
	return fn(p)
}

// feeForTikTok applies the operator-overridable commission with
// clamping into the [2%, 5%] band per the EC-6-2 spec.
func feeForTikTok(p PriceComponents) (int, error) {
	pct := clampTikTokCommission(p.TikTokCommissionPct)
	return roundToCents(float64(p.SellingPriceAUDCents) * pct), nil
}

// feeForFacebook is a flat 5% per the EC-6-2 spec.
func feeForFacebook(p PriceComponents) (int, error) {
	return roundToCents(float64(p.SellingPriceAUDCents) * FacebookCommissionPct), nil
}

// feeForWooCommerce applies Stripe AU pricing: 1.75% variable +
// A$0.30 fixed.
func feeForWooCommerce(p PriceComponents) (int, error) {
	variable := roundToCents(float64(p.SellingPriceAUDCents) * StripePctRate)
	return variable + StripeFixedAUDCents, nil
}

// feeForRedNote is intentionally unsupported in v3.5.0.
func feeForRedNote(_ PriceComponents) (int, error) {
	return 0, fmt.Errorf("%w: %q (uiauto-only channel; fee model deferred)", ErrUnsupportedChannel, ChannelRedNote)
}

// clampTikTokCommission applies the v3.5.0 EC-6-2 [2%, 5%] band.
// Zero / negative falls back to the default 5%; values above the
// ceiling clamp DOWN; values below the floor clamp UP.
func clampTikTokCommission(pct float64) float64 {
	if pct <= 0 {
		return DefaultTikTokCommissionPct
	}
	if pct < MinTikTokCommissionPct {
		return MinTikTokCommissionPct
	}
	if pct > MaxTikTokCommissionPct {
		return MaxTikTokCommissionPct
	}
	return pct
}

// convertCNYToAUDCents applies the FX rate. Pure; rounds to nearest
// cent so the integer arithmetic in assembleMargin stays clean.
func convertCNYToAUDCents(cnyCents int, audPerCNY float64) int {
	if cnyCents == 0 {
		return 0
	}
	return roundToCents(float64(cnyCents) * audPerCNY)
}

// assembleMargin builds the MarginResult from validated inputs.
// Pure; no branching beyond a single division-by-zero guard.
func assembleMargin(p PriceComponents, rate FXRate, costAUD, fee int) MarginResult {
	net := p.SellingPriceAUDCents - costAUD - fee - p.ShippingEstAUDCents
	pct := 0.0
	if p.SellingPriceAUDCents > 0 {
		pct = round4(float64(net) / float64(p.SellingPriceAUDCents))
	}
	return MarginResult{
		Channel:              p.Channel,
		SellingPriceAUDCents: p.SellingPriceAUDCents,
		CostAUDCents:         costAUD,
		PlatformFeeAUDCents:  fee,
		ShippingEstAUDCents:  p.ShippingEstAUDCents,
		NetAUDCents:          net,
		GrossMarginPct:       pct,
		FXRate:               rate,
	}
}

// roundToCents banker-rounds the supplied currency value to the
// nearest integer cent. math.Round handles the half-up case which
// matches the EC-6-2 acceptance scenarios.
func roundToCents(v float64) int {
	return int(math.Round(v))
}

// round4 truncates a float to 4 decimal places so the gross margin
// percentage in MarginResult is bit-exact across platforms.
func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

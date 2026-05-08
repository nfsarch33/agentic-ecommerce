package membership

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidBillingCycle is returned when the cycle value is unknown.
var ErrInvalidBillingCycle = errors.New("invalid billing cycle")

// BillingCycle is the cadence at which a MembershipPlan is renewed.
// Stored as a typed string so we never compare against magic strings.
type BillingCycle string

const (
	BillingCycleMonthly   BillingCycle = "monthly"
	BillingCycleQuarterly BillingCycle = "quarterly"
	BillingCycleAnnual    BillingCycle = "annual"
)

// ParseBillingCycle normalises and validates a string into a BillingCycle.
func ParseBillingCycle(value string) (BillingCycle, error) {
	normalised := BillingCycle(strings.ToLower(strings.TrimSpace(value)))
	switch normalised {
	case BillingCycleMonthly, BillingCycleQuarterly, BillingCycleAnnual:
		return normalised, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidBillingCycle, value)
	}
}

// String returns the canonical string representation of a BillingCycle.
func (b BillingCycle) String() string { return string(b) }

// Duration returns the wall-clock duration this BillingCycle represents.
//
// We use 30/90/365 day approximations so workflow timers and renewal
// scheduling stay deterministic across replays. Real billing cycle math
// (calendar months) lives in the Stripe adapter.
func (b BillingCycle) Duration() time.Duration {
	switch b {
	case BillingCycleMonthly:
		return 30 * 24 * time.Hour
	case BillingCycleQuarterly:
		return 90 * 24 * time.Hour
	case BillingCycleAnnual:
		return 365 * 24 * time.Hour
	default:
		return 0
	}
}

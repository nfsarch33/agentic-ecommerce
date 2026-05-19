// Package stripe provides a deterministic in-process stub of the Stripe
// API surface needed by the membership bounded context.
//
// Real Stripe integration lands in v2.5.0 (per the EC stack v3.0.0 plan).
// For v2.2.0 we ship a typed stub that returns deterministic identifiers
// derived from the request fields so:
//   - Workflow replay tests have stable golden histories.
//   - Frontend e2e tests can assert against predictable ids.
//   - Production code paths are exercised end-to-end without leaving the
//     network boundary.
package stripe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// Clock is the minimal time abstraction used by the stub so workflow
// determinism tests can supply a fixed clock.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// PaymentGateway is the deterministic stub used in dev compose, unit
// tests, and the workflow replay harness.
type PaymentGateway struct {
	mu     sync.RWMutex
	cache  map[string]port.PaymentSubscriptionStatus
	clock  Clock
	prefix string
}

// Config customises the stub.
type Config struct {
	Clock    Clock
	IDPrefix string // defaults to "sub_dev_".
}

// NewPaymentGateway builds a deterministic Stripe stub.
func NewPaymentGateway(cfg Config) *PaymentGateway {
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}
	prefix := cfg.IDPrefix
	if prefix == "" {
		prefix = "sub_dev_"
	}
	return &PaymentGateway{
		cache:  make(map[string]port.PaymentSubscriptionStatus),
		clock:  clock,
		prefix: prefix,
	}
}

// CreateSubscription records a stub Stripe subscription and returns
// deterministic identifiers derived from the request.
func (g *PaymentGateway) CreateSubscription(_ context.Context, req port.CreateSubscriptionRequest) (port.CreateSubscriptionResponse, error) {
	if err := validateCreateRequest(req); err != nil {
		return port.CreateSubscriptionResponse{}, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	stripeSubID := g.deterministicID(req.TenantID, req.SubscriptionID.String(), "sub")
	stripeCustID := g.deterministicID(req.TenantID, req.MemberEmail, "cus")

	now := g.clock.Now().UTC()
	periodEnd := now.Add(req.BillingCycle.Duration())
	if req.TrialDays > 0 {
		periodEnd = now.Add(time.Duration(req.TrialDays) * 24 * time.Hour)
	}

	g.cache[stripeSubID] = port.PaymentSubscriptionStatus{
		StripeSubscriptionID: stripeSubID,
		Status:               "active",
		CurrentPeriodEnd:     periodEnd,
		CancelAtPeriodEnd:    false,
	}
	return port.CreateSubscriptionResponse{
		StripeSubscriptionID: stripeSubID,
		StripeCustomerID:     stripeCustID,
		CurrentPeriodEnd:     periodEnd,
	}, nil
}

// CancelSubscription marks the stub subscription as canceled.
func (g *PaymentGateway) CancelSubscription(_ context.Context, req port.CancelSubscriptionRequest) error {
	if strings.TrimSpace(req.StripeSubscriptionID) == "" {
		return ErrMissingStripeID
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	status, ok := g.cache[req.StripeSubscriptionID]
	if !ok {
		// Stripe returns success on cancel-of-unknown to keep workflow
		// retries safe; we mirror that behaviour for the stub.
		return nil
	}
	status.Status = "canceled"
	status.CancelAtPeriodEnd = true
	g.cache[req.StripeSubscriptionID] = status
	return nil
}

// GetSubscription returns the cached status of a stub subscription.
func (g *PaymentGateway) GetSubscription(_ context.Context, req port.GetSubscriptionRequest) (port.PaymentSubscriptionStatus, error) {
	if strings.TrimSpace(req.StripeSubscriptionID) == "" {
		return port.PaymentSubscriptionStatus{}, ErrMissingStripeID
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	status, ok := g.cache[req.StripeSubscriptionID]
	if !ok {
		return port.PaymentSubscriptionStatus{}, ErrStripeSubscriptionNotFound
	}
	return status, nil
}

// ErrMissingStripeID is returned when a Stripe subscription id is empty.
var ErrMissingStripeID = newGatewayError("stripe subscription id is required")

// ErrStripeSubscriptionNotFound is returned when GetSubscription cannot
// find a cached subscription. The cancel path stays idempotent.
var ErrStripeSubscriptionNotFound = newGatewayError("stripe subscription not found")

type gatewayError struct{ msg string }

func newGatewayError(msg string) error { return &gatewayError{msg: msg} }

func (e *gatewayError) Error() string { return "stripe stub: " + e.msg }

func validateCreateRequest(req port.CreateSubscriptionRequest) error {
	if strings.TrimSpace(req.TenantID) == "" {
		return newGatewayError("tenant id is required")
	}
	if strings.TrimSpace(req.MemberEmail) == "" {
		return newGatewayError("member email is required")
	}
	if strings.TrimSpace(req.StripePriceID) == "" {
		return newGatewayError("stripe price id is required")
	}
	if req.BillingCycle == "" {
		return newGatewayError("billing cycle is required")
	}
	return nil
}

// deterministicID hashes (tenant, key, kind) into a stable lowercase
// hex prefix of length 16 plus the configured stub prefix. Identical
// inputs always yield identical ids so workflow replay tests are
// hermetic.
func (g *PaymentGateway) deterministicID(tenant, key, kind string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s", tenant, key, kind)))
	return g.prefix + hex.EncodeToString(h[:8])
}

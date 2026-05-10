// File scope: v4.8.0 Story 4 -- commission engine.
//
// Calculates per-vendor commission on orders and tracks payouts.
//
// Decomposition discipline (HARD GATE: complex_fn=4):
//   - Calculate     -> lookupRate + computeAmount (cyclomatic 3)
//   - RecordPayout  -> validate + persist (cyclomatic 2)
//   - Report        -> query + aggregate (cyclomatic 2)
package marketplace

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrPayoutNotFound    = errors.New("marketplace: payout not found")
	ErrPayoutInvalidArgs = errors.New("marketplace: payout invalid arguments")
)

type PayoutStatus string

const (
	PayoutStatusPending PayoutStatus = "pending"
	PayoutStatusPaid    PayoutStatus = "paid"
	PayoutStatusFailed  PayoutStatus = "failed"
)

type CommissionResult struct {
	VendorID          string `json:"vendor_id"`
	OrderTotalCents   int64  `json:"order_total_cents"`
	CommissionCents   int64  `json:"commission_cents"`
	VendorPayoutCents int64  `json:"vendor_payout_cents"`
	RateBPS           int    `json:"rate_bps"`
}

type VendorPayout struct {
	PayoutID    string       `json:"payout_id"`
	VendorID    string       `json:"vendor_id"`
	TenantID    string       `json:"tenant_id"`
	AmountCents int64        `json:"amount_cents"`
	Status      PayoutStatus `json:"status"`
	PeriodStart time.Time    `json:"period_start"`
	PeriodEnd   time.Time    `json:"period_end"`
	CreatedAt   time.Time    `json:"created_at"`
}

type CommissionReport struct {
	VendorID         string    `json:"vendor_id"`
	TenantID         string    `json:"tenant_id"`
	TotalOrdersCents int64     `json:"total_orders_cents"`
	TotalCommission  int64     `json:"total_commission_cents"`
	TotalPayout      int64     `json:"total_payout_cents"`
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
}

type PayoutStore interface {
	SavePayout(ctx context.Context, payout VendorPayout) error
	ListPayouts(ctx context.Context, tenantID, vendorID string, from, to time.Time) ([]VendorPayout, error)
}

type CommissionMetrics interface {
	RecordCommission(tenantID, vendorID string, amountCents int64)
}

type CommissionEngineConfig struct {
	VendorStore VendorStore
	PayoutStore PayoutStore
	Metrics     CommissionMetrics
	IDFunc      func() string
	Now         func() time.Time
}

type CommissionEngine struct {
	vendors VendorStore
	payouts PayoutStore
	metrics CommissionMetrics
	idFunc  func() string
	now     func() time.Time
}

func NewCommissionEngine(cfg CommissionEngineConfig) *CommissionEngine {
	if cfg.IDFunc == nil {
		cfg.IDFunc = func() string { return fmt.Sprintf("po-%d", time.Now().UnixNano()) }
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &CommissionEngine{
		vendors: cfg.VendorStore,
		payouts: cfg.PayoutStore,
		metrics: cfg.Metrics,
		idFunc:  cfg.IDFunc,
		now:     cfg.Now,
	}
}

func (e *CommissionEngine) Calculate(ctx context.Context, tenantID, vendorID string, orderTotalCents int64) (CommissionResult, error) {
	rate, err := e.lookupRate(ctx, tenantID, vendorID)
	if err != nil {
		return CommissionResult{}, err
	}
	commission := computeCommission(orderTotalCents, rate)
	e.recordMetric(tenantID, vendorID, commission)
	return CommissionResult{
		VendorID:          vendorID,
		OrderTotalCents:   orderTotalCents,
		CommissionCents:   commission,
		VendorPayoutCents: orderTotalCents - commission,
		RateBPS:           rate,
	}, nil
}

func (e *CommissionEngine) RecordPayout(ctx context.Context, tenantID, vendorID string, amountCents int64, periodStart, periodEnd time.Time) (VendorPayout, error) {
	if amountCents < 0 {
		return VendorPayout{}, fmt.Errorf("%w: negative amount", ErrPayoutInvalidArgs)
	}
	payout := VendorPayout{
		PayoutID:    e.idFunc(),
		VendorID:    vendorID,
		TenantID:    tenantID,
		AmountCents: amountCents,
		Status:      PayoutStatusPending,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		CreatedAt:   e.now(),
	}
	if err := e.payouts.SavePayout(ctx, payout); err != nil {
		return VendorPayout{}, err
	}
	return payout, nil
}

func (e *CommissionEngine) Report(ctx context.Context, tenantID, vendorID string, from, to time.Time) (CommissionReport, error) {
	payouts, err := e.payouts.ListPayouts(ctx, tenantID, vendorID, from, to)
	if err != nil {
		return CommissionReport{}, err
	}
	return aggregatePayouts(tenantID, vendorID, from, to, payouts), nil
}

func (e *CommissionEngine) lookupRate(ctx context.Context, tenantID, vendorID string) (int, error) {
	v, err := e.vendors.Get(ctx, tenantID, vendorID)
	if err != nil {
		return 0, err
	}
	return v.CommissionRateBPS, nil
}

func computeCommission(orderTotalCents int64, rateBPS int) int64 {
	return orderTotalCents * int64(rateBPS) / 10000
}

func (e *CommissionEngine) recordMetric(tenantID, vendorID string, amountCents int64) {
	if e.metrics == nil {
		return
	}
	e.metrics.RecordCommission(tenantID, vendorID, amountCents)
}

func aggregatePayouts(tenantID, vendorID string, from, to time.Time, payouts []VendorPayout) CommissionReport {
	var totalPayout int64
	for _, p := range payouts {
		totalPayout += p.AmountCents
	}
	return CommissionReport{
		VendorID:    vendorID,
		TenantID:    tenantID,
		TotalPayout: totalPayout,
		PeriodStart: from,
		PeriodEnd:   to,
	}
}

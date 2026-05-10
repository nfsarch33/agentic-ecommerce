// File scope: v3.8.1 carry-forward closure -- production adapter
// for the v3.8.0 EC-7-5 workflow.ReturnLabelGenerator port.
//
// The returns saga workflow consumes ReturnLabelGenerator to
// produce the reverse-direction shipping label (customer ships
// the returned item back to the supplier or warehouse). Production
// wires the existing v3.8.0 fulfilment.ShippingLabelGenerator
// through this thin adapter so the saga does not directly depend
// on the fulfilment package (keeping the workflow package's
// import surface flat).
//
// CancelLabel is a best-effort op: the underlying carrier API
// supports cancellation only within a small window after label
// creation; the adapter records the intent + lets the operator
// dashboard chase the carrier post-hoc when the cancellation
// window has elapsed.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/agent/fulfilment"
	"github.com/nfsarch33/agentic-ecommerce/internal/workflow"
)

// ErrReturnLabelGeneratorUnconfigured is returned by
// NewReturnLabelGeneratorAdapter when the underlying generator is
// not provided.
var ErrReturnLabelGeneratorUnconfigured = errors.New("postgres: return label generator unconfigured")

// ReturnLabelGeneratorAdapter is the v3.8.1 production adapter
// that maps workflow.ReturnsSagaWorkflowInput to the v3.8.0
// fulfilment.ShipmentRequest envelope and wraps the resulting
// LabelResult back into a workflow.ReturnLabelResult.
type ReturnLabelGeneratorAdapter struct {
	pool          productStore
	labelGen      *fulfilment.ShippingLabelGenerator
	originPost    string
	originCountry string
	weightGrams   int
}

// ReturnLabelGeneratorConfig wires an adapter.
type ReturnLabelGeneratorConfig struct {
	// LabelGen is the v3.8.0 ShippingLabelGenerator (carries the
	// AusPost + DHL clients).
	LabelGen *fulfilment.ShippingLabelGenerator
	// OriginPost defaults the source postcode for the reverse
	// label; production sets this to the warehouse postcode.
	OriginPost string
	// OriginCountry defaults the source country (typically AU).
	OriginCountry string
	// WeightGrams defaults the package weight when the workflow
	// input does not carry a weight; production-typical 250g.
	WeightGrams int
}

// NewReturnLabelGeneratorAdapter constructs the adapter. A nil
// pool is permitted; CancelLabel surfaces a no-op in that case so
// the workflow tests can drive the adapter without a Postgres
// container. Production composition wires a real *pgxpool.Pool.
func NewReturnLabelGeneratorAdapter(pool *pgxpool.Pool, cfg ReturnLabelGeneratorConfig) (*ReturnLabelGeneratorAdapter, error) {
	if cfg.LabelGen == nil {
		return nil, fmt.Errorf("%w: LabelGen required", ErrReturnLabelGeneratorUnconfigured)
	}
	if cfg.OriginCountry == "" {
		cfg.OriginCountry = "AU"
	}
	if cfg.OriginPost == "" {
		cfg.OriginPost = "3000"
	}
	if cfg.WeightGrams <= 0 {
		cfg.WeightGrams = 250
	}
	// Convert typed-nil to interface-nil so CancelLabel's
	// `if a.pool == nil` guard fires correctly when the caller
	// passes a nil *pgxpool.Pool.
	var pStore productStore
	if pool != nil {
		pStore = pool
	}
	return &ReturnLabelGeneratorAdapter{
		pool:          pStore,
		labelGen:      cfg.LabelGen,
		originPost:    cfg.OriginPost,
		originCountry: cfg.OriginCountry,
		weightGrams:   cfg.WeightGrams,
	}, nil
}

// GenerateReturnLabel implements workflow.ReturnLabelGenerator.
// Cyclomatic 3 (build envelope + delegate + adapt result).
func (a *ReturnLabelGeneratorAdapter) GenerateReturnLabel(ctx context.Context, in workflow.ReturnsSagaWorkflowInput) (workflow.ReturnLabelResult, error) {
	req := fulfilment.ShipmentRequest{
		TenantID:      in.TenantID,
		OrderID:       "RMA-" + in.RMAID,
		BuyerEmail:    in.BuyerEmail,
		OriginCountry: a.originCountry,
		OriginPost:    a.originPost,
		DestCountry:   a.originCountry,
		DestPost:      a.originPost,
		WeightGrams:   a.weightGrams,
	}
	res, err := a.labelGen.Generate(ctx, req)
	if err != nil {
		return workflow.ReturnLabelResult{}, fmt.Errorf("postgres: return label generate: %w", err)
	}
	return workflow.ReturnLabelResult{
		Carrier:        res.Carrier,
		TrackingNumber: res.TrackingNumber,
		LabelPDFURL:    res.LabelPDFURL,
		CostAUDCents:   res.CostAUDCents,
	}, nil
}

// CancelLabel implements workflow.ReturnLabelGenerator. Best-effort
// op: marks the shipping_labels row status='cancelled'. The
// underlying carrier API may or may not honour the cancellation
// depending on how long ago the label was issued; the operator
// dashboard chases stragglers post-hoc.
func (a *ReturnLabelGeneratorAdapter) CancelLabel(ctx context.Context, in workflow.ReturnsSagaWorkflowInput) error {
	if a.pool == nil {
		// Pool is optional in tests; the workflow tests use a
		// fake; production composition supplies the pool. Surface
		// nil so the saga rollback never blocks on a missing pool.
		return nil
	}
	const q = `
		UPDATE shipping_labels
		SET status = 'cancelled', updated_at = NOW()
		WHERE tenant_id = $1
		  AND order_id = $2`
	_, err := a.pool.Exec(ctx, q, in.TenantID, "RMA-"+in.RMAID)
	if err != nil {
		return fmt.Errorf("postgres: return label cancel: %w", err)
	}
	return nil
}

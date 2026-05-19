package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nfsarch33/helixon-ec/internal/adapter/carrier"
	"github.com/nfsarch33/helixon-ec/internal/agent/fulfilment"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/workflow"
	"github.com/stretchr/testify/require"
)

// fakeReturnLabelBus is a no-op publisher so the embedded
// ShippingLabelGenerator can run end-to-end during the adapter
// test.
type fakeReturnLabelBus struct{}

func (b *fakeReturnLabelBus) Publish(_ context.Context, _ eventbus.Event) error { return nil }

func (b *fakeReturnLabelBus) Close() error { return nil }

// fakeReturnLabelCarrier is a stub CarrierClient so the test can
// drive the embedded ShippingLabelGenerator without network calls.
type fakeReturnLabelCarrier struct {
	name string
	err  error
}

func (c *fakeReturnLabelCarrier) Name() string { return c.name }

func (c *fakeReturnLabelCarrier) Quote(_ context.Context, _ carrier.QuoteRequest) (carrier.Quote, error) {
	if c.err != nil {
		return carrier.Quote{}, c.err
	}
	return carrier.Quote{Carrier: c.name, CostAUDCents: 1099, ETADays: 4}, nil
}

func (c *fakeReturnLabelCarrier) CreateLabel(_ context.Context, _ carrier.LabelRequest) (carrier.Label, error) {
	if c.err != nil {
		return carrier.Label{}, c.err
	}
	return carrier.Label{
		Carrier:        c.name,
		TrackingNumber: c.name + "-RET-001",
		LabelPDFURL:    "https://" + c.name + "/ret.pdf",
		CostAUDCents:   1099,
		ETADays:        4,
	}, nil
}

// fakeUpdatePool stubs the productStore Exec method so the adapter
// CancelLabel path can be tested without a Postgres container.
type fakeUpdatePool struct {
	calls int
	lastQ string
	args  []any
	err   error
}

func (p *fakeUpdatePool) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	p.calls++
	p.lastQ = q
	p.args = args
	return pgconn.CommandTag{}, p.err
}

func (p *fakeUpdatePool) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, errors.New("not used")
}

func (p *fakeUpdatePool) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return nil
}

func newReturnLabelGenForTest(t *testing.T, c fulfilment.CarrierClient) *fulfilment.ShippingLabelGenerator {
	t.Helper()
	gen, err := fulfilment.NewShippingLabelGenerator(nil, fulfilment.ShippingLabelConfig{
		Carriers:  []fulfilment.CarrierClient{c},
		Publisher: &fakeReturnLabelBus{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gen.Close(context.Background()) })
	return gen
}

func TestReturnLabelGeneratorAdapter_GenerateReturnLabel(t *testing.T) {
	t.Parallel()
	gen := newReturnLabelGenForTest(t, &fakeReturnLabelCarrier{name: carrier.CarrierAusPost})
	adapter, err := NewReturnLabelGeneratorAdapter(nil, ReturnLabelGeneratorConfig{LabelGen: gen})
	require.NoError(t, err)
	res, err := adapter.GenerateReturnLabel(context.Background(), workflow.ReturnsSagaWorkflowInput{
		TenantID:   "tenant-A",
		RMAID:      "rma-001",
		BuyerEmail: "buyer@example.test",
	})
	require.NoError(t, err)
	require.Equal(t, carrier.CarrierAusPost, res.Carrier)
	require.Equal(t, carrier.CarrierAusPost+"-RET-001", res.TrackingNumber)
}

func TestReturnLabelGeneratorAdapter_GenerateReturnLabel_AllCarriersFailed(t *testing.T) {
	t.Parallel()
	gen := newReturnLabelGenForTest(t, &fakeReturnLabelCarrier{name: carrier.CarrierAusPost, err: errors.New("dead")})
	adapter, err := NewReturnLabelGeneratorAdapter(nil, ReturnLabelGeneratorConfig{LabelGen: gen})
	require.NoError(t, err)
	_, err = adapter.GenerateReturnLabel(context.Background(), workflow.ReturnsSagaWorkflowInput{
		TenantID:   "tenant-A",
		RMAID:      "rma-002",
		BuyerEmail: "buyer@example.test",
	})
	require.Error(t, err, "all-carriers-failed surface must propagate")
}

func TestReturnLabelGeneratorAdapter_CancelLabel_WritesUpdate(t *testing.T) {
	t.Parallel()
	pool := &fakeUpdatePool{}
	gen := newReturnLabelGenForTest(t, &fakeReturnLabelCarrier{name: carrier.CarrierAusPost})
	adapter := &ReturnLabelGeneratorAdapter{pool: pool, labelGen: gen, originPost: "3000", originCountry: "AU", weightGrams: 250}
	err := adapter.CancelLabel(context.Background(), workflow.ReturnsSagaWorkflowInput{TenantID: "tenant-A", RMAID: "rma-001"})
	require.NoError(t, err)
	require.Equal(t, 1, pool.calls)
	require.Contains(t, pool.lastQ, "UPDATE shipping_labels")
	require.Equal(t, []any{"tenant-A", "RMA-rma-001"}, pool.args)
}

func TestReturnLabelGeneratorAdapter_CancelLabel_NilPoolNoOp(t *testing.T) {
	t.Parallel()
	gen := newReturnLabelGenForTest(t, &fakeReturnLabelCarrier{name: carrier.CarrierAusPost})
	adapter, err := NewReturnLabelGeneratorAdapter(nil, ReturnLabelGeneratorConfig{LabelGen: gen})
	require.NoError(t, err)
	require.NoError(t, adapter.CancelLabel(context.Background(), workflow.ReturnsSagaWorkflowInput{TenantID: "tenant-A", RMAID: "rma-001"}))
}

func TestReturnLabelGeneratorAdapter_CancelLabel_DBError(t *testing.T) {
	t.Parallel()
	pool := &fakeUpdatePool{err: errors.New("conn refused")}
	gen := newReturnLabelGenForTest(t, &fakeReturnLabelCarrier{name: carrier.CarrierAusPost})
	adapter := &ReturnLabelGeneratorAdapter{pool: pool, labelGen: gen, originPost: "3000", originCountry: "AU", weightGrams: 250}
	err := adapter.CancelLabel(context.Background(), workflow.ReturnsSagaWorkflowInput{TenantID: "tenant-A", RMAID: "rma-002"})
	require.Error(t, err)
}

func TestReturnLabelGeneratorAdapter_RequiresLabelGen(t *testing.T) {
	t.Parallel()
	_, err := NewReturnLabelGeneratorAdapter(nil, ReturnLabelGeneratorConfig{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrReturnLabelGeneratorUnconfigured))
}

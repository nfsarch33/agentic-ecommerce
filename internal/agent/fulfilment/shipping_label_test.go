package fulfilment

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/carrier"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/stretchr/testify/require"
)

type stubCarrier struct {
	name       string
	quote      carrier.Quote
	quoteErr   error
	label      carrier.Label
	labelErr   error
	quoteCalls int
	labelCalls int
	mu         sync.Mutex
}

func (s *stubCarrier) Name() string { return s.name }

func (s *stubCarrier) Quote(_ context.Context, _ carrier.QuoteRequest) (carrier.Quote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quoteCalls++
	if s.quoteErr != nil {
		return carrier.Quote{}, s.quoteErr
	}
	return s.quote, nil
}

func (s *stubCarrier) CreateLabel(_ context.Context, _ carrier.LabelRequest) (carrier.Label, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.labelCalls++
	if s.labelErr != nil {
		return carrier.Label{}, s.labelErr
	}
	return s.label, nil
}

type captureBus struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (b *captureBus) Publish(_ context.Context, evt eventbus.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, evt)
	return nil
}

func (b *captureBus) Close() error { return nil }

func (b *captureBus) Snapshot() []eventbus.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]eventbus.Event, len(b.events))
	copy(out, b.events)
	return out
}

type capturingMetrics struct {
	mu           sync.Mutex
	labelStatus  []string
	labelCarrier []string
	costObserved []int
}

func (m *capturingMetrics) RecordShippingLabel(_, c, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.labelStatus = append(m.labelStatus, status)
	m.labelCarrier = append(m.labelCarrier, c)
}

func (m *capturingMetrics) ObserveShippingLabelCost(_, _ string, cents int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.costObserved = append(m.costObserved, cents)
}

func newAusPostStub(cost, eta int) *stubCarrier {
	return &stubCarrier{
		name:  carrier.CarrierAusPost,
		quote: carrier.Quote{Carrier: carrier.CarrierAusPost, CostAUDCents: cost, ETADays: eta},
		label: carrier.Label{Carrier: carrier.CarrierAusPost, TrackingNumber: "AP-XYZ", LabelPDFURL: "https://ap/xyz.pdf", CostAUDCents: cost, ETADays: eta, GeneratedAt: time.Unix(0, 0).UTC()},
	}
}

func newDHLStub(cost, eta int) *stubCarrier {
	return &stubCarrier{
		name:  carrier.CarrierDHL,
		quote: carrier.Quote{Carrier: carrier.CarrierDHL, CostAUDCents: cost, ETADays: eta},
		label: carrier.Label{Carrier: carrier.CarrierDHL, TrackingNumber: "DHL-9", LabelPDFURL: "https://dhl/9.pdf", CostAUDCents: cost, ETADays: eta, GeneratedAt: time.Unix(0, 0).UTC()},
	}
}

func defaultRequest() ShipmentRequest {
	return ShipmentRequest{
		TenantID:      "tenant-a",
		OrderID:       "ord-123",
		BuyerEmail:    "buyer@example.test",
		OriginCountry: "AU",
		OriginPost:    "3000",
		DestCountry:   "AU",
		DestPost:      "2000",
		WeightGrams:   500,
	}
}

func TestShippingLabel_GeneratesViaAusPost(t *testing.T) {
	t.Parallel()
	ap := newAusPostStub(1299, 4)
	dhl := newDHLStub(2599, 3)
	bus := &captureBus{}
	gen, err := NewShippingLabelGenerator(nil, ShippingLabelConfig{
		Carriers:  []CarrierClient{ap, dhl},
		Publisher: bus,
		Now:       func() time.Time { return time.Unix(1000, 0).UTC() },
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gen.Close(context.Background()) })

	res, err := gen.Generate(context.Background(), defaultRequest())
	require.NoError(t, err)
	require.Equal(t, carrier.CarrierAusPost, res.Carrier, "AusPost is cheapest at 1299 < 2599")
	require.Equal(t, "AP-XYZ", res.TrackingNumber)
	require.Equal(t, 1299, res.CostAUDCents)
	require.False(t, res.Cached)
	require.Equal(t, 1, ap.labelCalls)
	require.Equal(t, 0, dhl.labelCalls)

	events := bus.Snapshot()
	require.Len(t, events, 1)
	require.Equal(t, eventbus.ShipmentLabelGenerated, events[0].Type)
}

func TestShippingLabel_GeneratesViaDHL(t *testing.T) {
	t.Parallel()
	ap := newAusPostStub(2999, 4)
	dhl := newDHLStub(2199, 3)
	gen, err := NewShippingLabelGenerator(nil, ShippingLabelConfig{
		Carriers:  []CarrierClient{ap, dhl},
		Publisher: &captureBus{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gen.Close(context.Background()) })

	res, err := gen.Generate(context.Background(), defaultRequest())
	require.NoError(t, err)
	require.Equal(t, carrier.CarrierDHL, res.Carrier, "DHL cheaper than AusPost in this scenario")
	require.Equal(t, "DHL-9", res.TrackingNumber)
	require.Equal(t, 2199, res.CostAUDCents)
}

func TestShippingLabel_PicksCheapestCarrier(t *testing.T) {
	t.Parallel()
	type scenario struct {
		name        string
		weight      int
		dest        string
		apCost      int
		dhlCost     int
		expectedCar string
	}
	scenarios := []scenario{
		{name: "domestic-AU-light-AP-cheaper", weight: 250, dest: "2000", apCost: 1099, dhlCost: 2199, expectedCar: carrier.CarrierAusPost},
		{name: "domestic-AU-medium-AP-cheaper", weight: 1000, dest: "2000", apCost: 1799, dhlCost: 2799, expectedCar: carrier.CarrierAusPost},
		{name: "domestic-AU-heavy-DHL-cheaper", weight: 5000, dest: "2000", apCost: 4599, dhlCost: 3799, expectedCar: carrier.CarrierDHL},
		{name: "international-light-DHL-cheaper", weight: 250, dest: "10001", apCost: 5599, dhlCost: 3299, expectedCar: carrier.CarrierDHL},
		{name: "international-medium-DHL-cheaper", weight: 1000, dest: "10001", apCost: 7299, dhlCost: 4399, expectedCar: carrier.CarrierDHL},
		{name: "international-heavy-DHL-cheaper", weight: 5000, dest: "10001", apCost: 12299, dhlCost: 9099, expectedCar: carrier.CarrierDHL},
	}
	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			ap := newAusPostStub(sc.apCost, 4)
			dhl := newDHLStub(sc.dhlCost, 3)
			gen, err := NewShippingLabelGenerator(nil, ShippingLabelConfig{
				Carriers:  []CarrierClient{ap, dhl},
				Publisher: &captureBus{},
			})
			require.NoError(t, err)
			t.Cleanup(func() { _ = gen.Close(context.Background()) })

			req := defaultRequest()
			req.WeightGrams = sc.weight
			req.DestPost = sc.dest
			req.OrderID = sc.name

			res, err := gen.Generate(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, sc.expectedCar, res.Carrier)
		})
	}
}

func TestShippingLabel_PicksAusPostOnTieDomesticAU(t *testing.T) {
	t.Parallel()
	ap := newAusPostStub(1299, 4)
	dhl := newDHLStub(1299, 3)
	gen, err := NewShippingLabelGenerator(nil, ShippingLabelConfig{
		Carriers:  []CarrierClient{ap, dhl},
		Publisher: &captureBus{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gen.Close(context.Background()) })

	res, err := gen.Generate(context.Background(), defaultRequest())
	require.NoError(t, err)
	require.Equal(t, carrier.CarrierAusPost, res.Carrier, "tie -> AusPost (domestic AU default)")
}

func TestShippingLabel_FallsBackToSecondCarrierOnPrimaryFail(t *testing.T) {
	t.Parallel()
	ap := newAusPostStub(1299, 4)
	ap.labelErr = errors.New("boom from AusPost label endpoint")
	dhl := newDHLStub(2599, 3)

	gen, err := NewShippingLabelGenerator(nil, ShippingLabelConfig{
		Carriers:  []CarrierClient{ap, dhl},
		Publisher: &captureBus{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gen.Close(context.Background()) })

	res, err := gen.Generate(context.Background(), defaultRequest())
	require.NoError(t, err)
	require.Equal(t, carrier.CarrierDHL, res.Carrier, "fallback to DHL when AusPost CreateLabel fails")
	require.Equal(t, 1, ap.labelCalls, "AusPost still attempted first")
	require.Equal(t, 1, dhl.labelCalls, "DHL fallback fired")
}

func TestShippingLabel_AllCarriersFailedReturnsError(t *testing.T) {
	t.Parallel()
	ap := newAusPostStub(1299, 4)
	ap.labelErr = errors.New("ap fail")
	dhl := newDHLStub(2599, 3)
	dhl.labelErr = errors.New("dhl fail")

	gen, err := NewShippingLabelGenerator(nil, ShippingLabelConfig{
		Carriers:  []CarrierClient{ap, dhl},
		Publisher: &captureBus{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gen.Close(context.Background()) })

	_, err = gen.Generate(context.Background(), defaultRequest())
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrAllCarriersFailed))
}

func TestShippingLabel_SLAConstraintEnforced(t *testing.T) {
	t.Parallel()
	// AusPost ETA 7 days (out of SLA), DHL ETA 8 days (out of SLA);
	// SLA=5 -> ErrSLANotMet because no carrier fits.
	ap := newAusPostStub(1299, 7)
	dhl := newDHLStub(2599, 8)
	gen, err := NewShippingLabelGenerator(nil, ShippingLabelConfig{
		Carriers:   []CarrierClient{ap, dhl},
		Publisher:  &captureBus{},
		DefaultSLA: 5,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gen.Close(context.Background()) })

	_, err = gen.Generate(context.Background(), defaultRequest())
	require.Error(t, err)
	require.True(t, errors.Is(err, carrier.ErrSLANotMet))
	require.True(t, errors.Is(err, ErrSLANotMet))
}

func TestShippingLabel_SLAExcludesOutOfWindowCarrier(t *testing.T) {
	t.Parallel()
	// DHL is cheaper but out of SLA -> AusPost wins.
	ap := newAusPostStub(2999, 4)
	dhl := newDHLStub(1299, 9)
	gen, err := NewShippingLabelGenerator(nil, ShippingLabelConfig{
		Carriers:   []CarrierClient{ap, dhl},
		Publisher:  &captureBus{},
		DefaultSLA: 5,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gen.Close(context.Background()) })

	res, err := gen.Generate(context.Background(), defaultRequest())
	require.NoError(t, err)
	require.Equal(t, carrier.CarrierAusPost, res.Carrier)
}

func TestShippingLabel_IdempotentReturnsCached(t *testing.T) {
	t.Parallel()
	ap := newAusPostStub(1299, 4)
	dhl := newDHLStub(2599, 3)
	gen, err := NewShippingLabelGenerator(nil, ShippingLabelConfig{
		Carriers:  []CarrierClient{ap, dhl},
		Publisher: &captureBus{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gen.Close(context.Background()) })

	first, err := gen.Generate(context.Background(), defaultRequest())
	require.NoError(t, err)
	require.False(t, first.Cached)

	second, err := gen.Generate(context.Background(), defaultRequest())
	require.NoError(t, err)
	require.True(t, second.Cached)
	require.Equal(t, first.TrackingNumber, second.TrackingNumber)
	require.Equal(t, 1, ap.labelCalls, "second call must hit cache, not the carrier")
}

func TestShippingLabel_ClosedReturnsError(t *testing.T) {
	t.Parallel()
	gen, err := NewShippingLabelGenerator(nil, ShippingLabelConfig{
		Carriers:  []CarrierClient{newAusPostStub(1299, 4)},
		Publisher: &captureBus{},
	})
	require.NoError(t, err)
	require.NoError(t, gen.Close(context.Background()))

	_, err = gen.Generate(context.Background(), defaultRequest())
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrShippingLabelGeneratorClosed))
}

func TestShippingLabel_RecordsKPIAndMetrics(t *testing.T) {
	t.Parallel()
	metrics := &capturingMetrics{}
	var samples []ShippingLabelKPISample
	var mu sync.Mutex
	hook := func(s ShippingLabelKPISample) {
		mu.Lock()
		defer mu.Unlock()
		samples = append(samples, s)
	}
	gen, err := NewShippingLabelGenerator(nil, ShippingLabelConfig{
		Carriers:  []CarrierClient{newAusPostStub(1299, 4)},
		Publisher: &captureBus{},
		Metrics:   metrics,
		KPIHook:   hook,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gen.Close(context.Background()) })

	_, err = gen.Generate(context.Background(), defaultRequest())
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, samples, 1)
	require.Equal(t, "generated", samples[0].Status)
	require.Equal(t, 1, len(metrics.labelStatus))
	require.Equal(t, "generated", metrics.labelStatus[0])
	require.Equal(t, []int{1299}, metrics.costObserved)
}

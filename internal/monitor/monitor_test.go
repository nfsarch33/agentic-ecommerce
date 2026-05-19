package monitor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/adapter/woocommerce"
)

type mockOrderClient struct {
	orders   []woocommerce.Order
	listErr  error
	lastOpts woocommerce.ListOptions
}

func (m *mockOrderClient) ListOrders(_ context.Context, opts woocommerce.ListOptions) ([]woocommerce.Order, error) {
	m.lastOpts = opts
	if m.listErr != nil {
		return nil, m.listErr
	}
	if opts.Status == "" {
		return m.orders, nil
	}
	filtered := make([]woocommerce.Order, 0, len(m.orders))
	for _, order := range m.orders {
		if order.Status == opts.Status {
			filtered = append(filtered, order)
		}
	}
	return filtered, nil
}

func TestPollReturnsFilteredOrders(t *testing.T) {
	t.Parallel()

	client := &mockOrderClient{orders: sampleOrders()}
	result := New(client, monitorLogger()).Run(context.Background(), Request{Action: "poll", Status: "processing", After: "2026-03-11T00:00:00"})

	if !result.Success || result.OrderCount != 1 {
		t.Fatalf("result = %+v", result)
	}
	if got := client.lastOpts.After; got != "2026-03-11T00:00:00" {
		t.Fatalf("after = %q", got)
	}
}

func TestSummaryAggregatesRevenueByStatus(t *testing.T) {
	t.Parallel()

	result := New(&mockOrderClient{orders: sampleOrders()}, monitorLogger()).Run(context.Background(), Request{Action: "summary"})

	if !result.Success {
		t.Fatalf("result = %+v", result)
	}
	if got := result.Summary["processing"]; got != 49.99 {
		t.Fatalf("processing revenue = %v", got)
	}
	if got := result.Summary["on-hold"]; got != 750 {
		t.Fatalf("on-hold revenue = %v", got)
	}
}

func TestAnomaliesFlagsRiskyOrders(t *testing.T) {
	t.Parallel()

	result := New(&mockOrderClient{orders: sampleOrders()}, monitorLogger()).Run(context.Background(), Request{
		Action:                "anomalies",
		HighValueThreshold:    500,
		HighQuantityThreshold: 10,
	})

	if !result.Success {
		t.Fatalf("result = %+v", result)
	}
	want := map[string]bool{"high_value": true, "on_hold": true, "failed": true, "high_quantity": true}
	for _, anomaly := range result.Anomalies {
		delete(want, anomaly.Type)
	}
	if len(want) != 0 {
		t.Fatalf("missing anomalies: %+v in %+v", want, result.Anomalies)
	}
}

func TestMonitorReportsUnknownActionAndAPIFailure(t *testing.T) {
	t.Parallel()

	if result := New(&mockOrderClient{}, monitorLogger()).Run(context.Background(), Request{Action: "bad"}); result.Success || result.Error == "" {
		t.Fatalf("unknown action result = %+v", result)
	}
	if result := New(&mockOrderClient{listErr: errors.New("connection refused")}, monitorLogger()).Run(context.Background(), Request{}); result.Success || result.Error == "" {
		t.Fatalf("api failure result = %+v", result)
	}
}

func sampleOrders() []woocommerce.Order {
	return []woocommerce.Order{
		{ID: 101, Status: "processing", Total: "49.99", Currency: "AUD", LineItems: []woocommerce.OrderLineItem{{ID: 1, Name: "Band", Quantity: 2, Total: "49.99"}}},
		{ID: 102, Status: "completed", Total: "120.00", Currency: "AUD", LineItems: []woocommerce.OrderLineItem{{ID: 2, Name: "Bottle", Quantity: 1, Total: "120.00"}}},
		{ID: 103, Status: "on-hold", Total: "750.00", Currency: "AUD", LineItems: []woocommerce.OrderLineItem{{ID: 3, Name: "Bulk", Quantity: 15, Total: "750.00"}}},
		{ID: 104, Status: "failed", Total: "25.00", Currency: "AUD", LineItems: []woocommerce.OrderLineItem{{ID: 4, Name: "Small", Quantity: 1, Total: "25.00"}}},
	}
}

func monitorLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

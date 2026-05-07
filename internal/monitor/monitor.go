package monitor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
)

const (
	defaultHighValueThreshold    = 500.0
	defaultHighQuantityThreshold = 10
)

type WooCommerceOrderClient interface {
	ListOrders(context.Context, woocommerce.ListOptions) ([]woocommerce.Order, error)
}

type Request struct {
	Action                string
	Status                string
	After                 string
	PerPage               int
	HighValueThreshold    float64
	HighQuantityThreshold int
}

type Anomaly struct {
	Type    string `json:"type"`
	OrderID int    `json:"order_id"`
	Reason  string `json:"reason"`
}

type Result struct {
	Success    bool                `json:"success"`
	OrderCount int                 `json:"order_count"`
	Orders     []woocommerce.Order `json:"orders,omitempty"`
	Summary    map[string]float64  `json:"summary,omitempty"`
	Anomalies  []Anomaly           `json:"anomalies,omitempty"`
	Error      string              `json:"error,omitempty"`
}

type Monitor struct {
	client WooCommerceOrderClient
	logger *slog.Logger
}

func New(client WooCommerceOrderClient, logger *slog.Logger) *Monitor {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Monitor{client: client, logger: logger}
}

func (m *Monitor) Run(ctx context.Context, req Request) Result {
	if req.Action == "" {
		req.Action = "poll"
	}
	switch req.Action {
	case "poll":
		return m.poll(ctx, req)
	case "summary":
		return m.summary(ctx, req)
	case "anomalies":
		return m.anomalies(ctx, req)
	default:
		return Result{Success: false, Error: fmt.Sprintf("unknown action: %s", req.Action)}
	}
}

func (m *Monitor) fetchOrders(ctx context.Context, req Request) ([]woocommerce.Order, error) {
	orders, err := m.client.ListOrders(ctx, woocommerce.ListOptions{
		PerPage: req.PerPage,
		Status:  req.Status,
		After:   req.After,
	})
	if err != nil {
		return nil, err
	}
	m.logger.Info("fetched orders", "count", len(orders), "status_filter", req.Status)
	return orders, nil
}

func (m *Monitor) poll(ctx context.Context, req Request) Result {
	orders, err := m.fetchOrders(ctx, req)
	if err != nil {
		return Result{Success: false, Error: err.Error()}
	}
	return Result{Success: true, OrderCount: len(orders), Orders: orders}
}

func (m *Monitor) summary(ctx context.Context, req Request) Result {
	orders, err := m.fetchOrders(ctx, req)
	if err != nil {
		return Result{Success: false, Error: err.Error()}
	}
	revenueByStatus := make(map[string]float64)
	for _, order := range orders {
		total, err := strconv.ParseFloat(order.Total, 64)
		if err != nil {
			m.logger.Warn("invalid order total", "order_id", order.ID, "total", order.Total)
			continue
		}
		revenueByStatus[order.Status] += total
	}
	return Result{Success: true, OrderCount: len(orders), Summary: revenueByStatus}
}

func (m *Monitor) anomalies(ctx context.Context, req Request) Result {
	orders, err := m.fetchOrders(ctx, req)
	if err != nil {
		return Result{Success: false, Error: err.Error()}
	}
	highValue := req.HighValueThreshold
	if highValue <= 0 {
		highValue = defaultHighValueThreshold
	}
	highQty := req.HighQuantityThreshold
	if highQty <= 0 {
		highQty = defaultHighQuantityThreshold
	}

	var anomalies []Anomaly
	for _, order := range orders {
		total, _ := strconv.ParseFloat(order.Total, 64)
		if total >= highValue {
			anomalies = append(anomalies, Anomaly{Type: "high_value", OrderID: order.ID, Reason: fmt.Sprintf("order total %.2f exceeds threshold %.2f", total, highValue)})
		}
		switch order.Status {
		case "on-hold":
			anomalies = append(anomalies, Anomaly{Type: "on_hold", OrderID: order.ID, Reason: "order is on hold"})
		case "failed":
			anomalies = append(anomalies, Anomaly{Type: "failed", OrderID: order.ID, Reason: "order payment failed"})
		case "cancelled":
			anomalies = append(anomalies, Anomaly{Type: "cancelled", OrderID: order.ID, Reason: "order was cancelled"})
		}
		for _, item := range order.LineItems {
			if item.Quantity >= highQty {
				anomalies = append(anomalies, Anomaly{Type: "high_quantity", OrderID: order.ID, Reason: fmt.Sprintf("item %q has quantity %d", item.Name, item.Quantity)})
				break
			}
		}
	}
	return Result{Success: true, OrderCount: len(orders), Orders: orders, Anomalies: anomalies}
}

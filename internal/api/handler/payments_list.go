package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// v4.3.0 payments list handler errors.
var (
	ErrPaymentsHandlerUnconfigured = errors.New("handler: payments handler unconfigured")
	ErrPaymentsHandlerClosed       = errors.New("handler: payments handler closed")
	ErrPaymentsTenantMissing       = errors.New("handler: payments tenant missing")
)

const (
	DefaultPaymentsLimit = 50
	MaxPaymentsLimit     = 200
)

// PaymentsFilter is the parsed query envelope.
type PaymentsFilter struct {
	TenantID string
	Status   string
	Provider string
	Limit    int
	Offset   int
}

// PaymentRow is a single payment record.
type PaymentRow struct {
	PaymentID   string    `json:"payment_id"`
	TenantID    string    `json:"tenant_id"`
	OrderID     string    `json:"order_id"`
	Provider    string    `json:"provider"`
	Status      string    `json:"status"`
	AmountCents int64     `json:"amount_cents"`
	Currency    string    `json:"currency"`
	CreatedAt   time.Time `json:"created_at"`
}

// PaymentsRepository is the port the handler reads through.
type PaymentsRepository interface {
	List(ctx context.Context, filter PaymentsFilter) ([]PaymentRow, int, error)
}

// PaymentsHandlerConfig wires the handler.
type PaymentsHandlerConfig struct {
	Repository   PaymentsRepository
	TenantHeader string
}

// PaymentsHandler is the v4.3.0 payments list HTTP handler.
type PaymentsHandler struct {
	repo         PaymentsRepository
	tenantHeader string
	logger       *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewPaymentsHandler constructs the handler.
func NewPaymentsHandler(logger *slog.Logger, cfg PaymentsHandlerConfig) (*PaymentsHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Repository == nil {
		return nil, fmt.Errorf("%w: PaymentsRepository required", ErrPaymentsHandlerUnconfigured)
	}
	if cfg.TenantHeader == "" {
		cfg.TenantHeader = "X-Tenant-Id"
	}
	return &PaymentsHandler{
		repo:         cfg.Repository,
		tenantHeader: cfg.TenantHeader,
		logger:       logger,
	}, nil
}

// Close marks the handler closed.
func (h *PaymentsHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// ServeHTTP handles GET /api/v1/payments.
func (h *PaymentsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.paymentsGuard(); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, err)
		return
	}
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
		return
	}
	h.handleList(w, r)
}

func (h *PaymentsHandler) paymentsGuard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrPaymentsHandlerClosed
	}
	return nil
}

func (h *PaymentsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parsePaymentsFilter(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	rows, total, err := h.repo.List(r.Context(), filter)
	if err != nil {
		h.logger.Error("payments.list_failed", "tenant_id", filter.TenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": filter.TenantID,
		"payments":  rows,
		"total":     total,
		"limit":     filter.Limit,
		"offset":    filter.Offset,
	})
}

func (h *PaymentsHandler) parsePaymentsFilter(r *http.Request) (PaymentsFilter, error) {
	tenantID, err := h.resolvePaymentsTenantID(r)
	if err != nil {
		return PaymentsFilter{}, err
	}
	q := r.URL.Query()
	limit := DefaultPaymentsLimit
	if raw := q.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return PaymentsFilter{}, fmt.Errorf("invalid limit")
		}
		if parsed > MaxPaymentsLimit {
			parsed = MaxPaymentsLimit
		}
		limit = parsed
	}
	offset := 0
	if raw := q.Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return PaymentsFilter{}, fmt.Errorf("invalid offset")
		}
		offset = parsed
	}
	return PaymentsFilter{
		TenantID: tenantID,
		Status:   strings.TrimSpace(q.Get("status")),
		Provider: strings.TrimSpace(q.Get("provider")),
		Limit:    limit,
		Offset:   offset,
	}, nil
}

func (h *PaymentsHandler) resolvePaymentsTenantID(r *http.Request) (string, error) {
	if v := strings.TrimSpace(r.Header.Get(h.tenantHeader)); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		return v, nil
	}
	return "", ErrPaymentsTenantMissing
}

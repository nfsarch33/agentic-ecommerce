package sync

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	stdsync "sync"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

type EventType string

const (
	EventProductImported     EventType = "product_imported"
	EventProductPublished    EventType = "product_published"
	EventInventoryReconciled EventType = "inventory_reconciled"
	EventConflictDetected    EventType = "conflict_detected"
	EventSyncFailed          EventType = "sync_failed"
)

const (
	ConflictPending  = "pending"
	ConflictResolved = "resolved"
)

type WooCommerceClient interface {
	ListProducts(context.Context, woocommerce.ListOptions) ([]woocommerce.Product, error)
	UpsertProduct(context.Context, catalog.Product) error
}

type Config struct {
	ProductRepository port.ProductRepository
	WooCommerce       WooCommerceClient
	DefaultCurrency   string
	Now               func() time.Time
}

type Engine struct {
	repo            port.ProductRepository
	wc              WooCommerceClient
	defaultCurrency string
	now             func() time.Time

	mu        stdsync.RWMutex
	events    []Event
	conflicts []Conflict
}

type ImportOptions struct {
	Page    int
	PerPage int
}

type ImportResult struct {
	Imported  int `json:"imported"`
	Conflicts int `json:"conflicts"`
}

type Status struct {
	TotalEvents      int       `json:"total_events"`
	PendingConflicts int       `json:"pending_conflicts"`
	LastEvent        *Event    `json:"last_event,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Event struct {
	ID        string            `json:"id"`
	Type      EventType         `json:"type"`
	ProductID string            `json:"product_id,omitempty"`
	RemoteID  int               `json:"remote_id,omitempty"`
	Message   string            `json:"message,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

type Conflict struct {
	ID         string          `json:"id"`
	ProductID  string          `json:"product_id,omitempty"`
	SKU        string          `json:"sku"`
	RemoteID   int             `json:"remote_id"`
	Status     string          `json:"status"`
	Fields     []ConflictField `json:"fields"`
	Resolution string          `json:"resolution,omitempty"`
	Note       string          `json:"note,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	ResolvedAt *time.Time      `json:"resolved_at,omitempty"`
}

type ConflictField struct {
	Field       string `json:"field"`
	LocalValue  string `json:"local_value"`
	RemoteValue string `json:"remote_value"`
}

func NewEngine(cfg Config) *Engine {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	currency := strings.ToUpper(strings.TrimSpace(cfg.DefaultCurrency))
	if currency == "" {
		currency = "AUD"
	}
	return &Engine{repo: cfg.ProductRepository, wc: cfg.WooCommerce, defaultCurrency: currency, now: now}
}

func (e *Engine) ImportFromWooCommerce(ctx context.Context, opts ImportOptions) (ImportResult, error) {
	if opts.PerPage <= 0 {
		opts.PerPage = 100
	}
	remoteProducts, err := e.wc.ListProducts(ctx, woocommerce.ListOptions{Page: opts.Page, PerPage: opts.PerPage})
	if err != nil {
		e.record(Event{Type: EventSyncFailed, Message: err.Error()})
		return ImportResult{}, err
	}

	localBySKU, err := e.localBySKU(ctx)
	if err != nil {
		e.record(Event{Type: EventSyncFailed, Message: err.Error()})
		return ImportResult{}, err
	}

	var result ImportResult
	for _, remote := range remoteProducts {
		local, exists := localBySKU[normalizeSKU(remote.SKU)]
		if exists {
			fields := e.DetectConflicts(local, remote)
			if len(fields) > 0 {
				e.addConflict(local, remote, fields)
				result.Conflicts++
			}
			continue
		}

		product, err := e.productFromWooCommerce(remote)
		if err != nil {
			e.record(Event{Type: EventSyncFailed, RemoteID: remote.ID, Message: err.Error()})
			continue
		}
		if err := e.repo.Create(ctx, product); err != nil {
			e.record(Event{Type: EventSyncFailed, ProductID: product.ID().String(), RemoteID: remote.ID, Message: err.Error()})
			continue
		}
		result.Imported++
		e.record(Event{Type: EventProductImported, ProductID: product.ID().String(), RemoteID: remote.ID, Message: "imported WooCommerce product"})
	}
	return result, nil
}

func (e *Engine) PublishToWooCommerce(ctx context.Context, id uuid.UUID) error {
	product, err := e.repo.GetByID(ctx, id)
	if err != nil {
		e.record(Event{Type: EventSyncFailed, ProductID: id.String(), Message: err.Error()})
		return err
	}
	if err := e.wc.UpsertProduct(ctx, product); err != nil {
		e.record(Event{Type: EventSyncFailed, ProductID: id.String(), Message: err.Error()})
		return err
	}
	e.record(Event{Type: EventProductPublished, ProductID: id.String(), Message: "published product to WooCommerce"})
	return nil
}

func (e *Engine) ReconcileInventory(ctx context.Context) (ImportResult, error) {
	result, err := e.ImportFromWooCommerce(ctx, ImportOptions{})
	if err != nil {
		return result, err
	}
	e.record(Event{Type: EventInventoryReconciled, Message: "inventory reconciliation completed", Metadata: map[string]string{
		"imported":  strconv.Itoa(result.Imported),
		"conflicts": strconv.Itoa(result.Conflicts),
	}})
	return result, nil
}

func (e *Engine) DetectConflicts(local catalog.Product, remote woocommerce.Product) []ConflictField {
	var fields []ConflictField
	if local.Title() != strings.TrimSpace(remote.Name) {
		fields = append(fields, ConflictField{Field: "title", LocalValue: local.Title(), RemoteValue: strings.TrimSpace(remote.Name)})
	}
	if local.Price().Amount() != e.remotePriceCents(remote) {
		fields = append(fields, ConflictField{Field: "price", LocalValue: strconv.Itoa(local.Price().Amount()), RemoteValue: strconv.Itoa(e.remotePriceCents(remote))})
	}
	if local.Stock() != remoteStock(remote) {
		fields = append(fields, ConflictField{Field: "stock", LocalValue: strconv.Itoa(local.Stock()), RemoteValue: strconv.Itoa(remoteStock(remote))})
	}
	if local.Description() != remoteDescription(remote) {
		fields = append(fields, ConflictField{Field: "description", LocalValue: local.Description(), RemoteValue: remoteDescription(remote)})
	}
	return fields
}

func (e *Engine) ResolveConflict(id, resolution, note string) (Conflict, error) {
	resolution = strings.TrimSpace(resolution)
	if resolution == "" {
		return Conflict{}, errors.New("resolution is required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	for idx := range e.conflicts {
		if e.conflicts[idx].ID == id {
			now := e.now()
			e.conflicts[idx].Status = ConflictResolved
			e.conflicts[idx].Resolution = resolution
			e.conflicts[idx].Note = strings.TrimSpace(note)
			e.conflicts[idx].ResolvedAt = &now
			return e.conflicts[idx], nil
		}
	}
	return Conflict{}, errors.New("conflict not found")
}

func (e *Engine) Events() []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Event, len(e.events))
	copy(out, e.events)
	return out
}

func (e *Engine) Conflicts() []Conflict {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Conflict, 0, len(e.conflicts))
	for _, conflict := range e.conflicts {
		if conflict.Status == ConflictPending {
			out = append(out, conflict)
		}
	}
	return out
}

func (e *Engine) Status() Status {
	e.mu.RLock()
	defer e.mu.RUnlock()
	status := Status{TotalEvents: len(e.events), UpdatedAt: e.now()}
	for _, conflict := range e.conflicts {
		if conflict.Status == ConflictPending {
			status.PendingConflicts++
		}
	}
	if len(e.events) > 0 {
		last := e.events[len(e.events)-1]
		status.LastEvent = &last
		if last.Type == EventSyncFailed {
			status.LastError = last.Message
		}
	}
	return status
}

func (e *Engine) RecordEvent(event Event) {
	e.record(event)
}

func (e *Engine) addConflict(local catalog.Product, remote woocommerce.Product, fields []ConflictField) {
	conflict := Conflict{
		ID:        uuid.NewString(),
		ProductID: local.ID().String(),
		SKU:       local.SKU(),
		RemoteID:  remote.ID,
		Status:    ConflictPending,
		Fields:    fields,
		CreatedAt: e.now(),
	}
	e.mu.Lock()
	e.conflicts = append(e.conflicts, conflict)
	e.mu.Unlock()
	e.record(Event{Type: EventConflictDetected, ProductID: local.ID().String(), RemoteID: remote.ID, Message: "manual sync conflict detected"})
}

func (e *Engine) record(event Event) {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = e.now()
	}
	e.mu.Lock()
	e.events = append(e.events, event)
	e.mu.Unlock()
}

func (e *Engine) localBySKU(ctx context.Context) (map[string]catalog.Product, error) {
	result, err := e.repo.List(ctx, 1, 10000)
	if err != nil {
		return nil, err
	}
	products := make(map[string]catalog.Product, len(result.Products))
	for _, product := range result.Products {
		products[normalizeSKU(product.SKU())] = product
	}
	return products, nil
}

func (e *Engine) productFromWooCommerce(remote woocommerce.Product) (catalog.Product, error) {
	price, err := catalog.NewMoney(e.remotePriceCents(remote), e.defaultCurrency)
	if err != nil {
		return catalog.Product{}, err
	}
	return catalog.NewProduct(catalog.ProductInput{
		SKU:         remote.SKU,
		Title:       remote.Name,
		Description: remoteDescription(remote),
		Price:       price,
		Stock:       remoteStock(remote),
		Status:      productStatus(remote.Status),
	})
}

func (e *Engine) remotePriceCents(remote woocommerce.Product) int {
	raw := strings.TrimSpace(remote.Regular)
	if raw == "" {
		raw = strings.TrimSpace(remote.Price)
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return int(math.Round(value * 100))
}

func normalizeSKU(sku string) string {
	return strings.ToUpper(strings.TrimSpace(sku))
}

func remoteDescription(remote woocommerce.Product) string {
	if strings.TrimSpace(remote.Description) != "" {
		return strings.TrimSpace(remote.Description)
	}
	return strings.TrimSpace(remote.ShortDesc)
}

func remoteStock(remote woocommerce.Product) int {
	if remote.StockQuantity == nil {
		return 0
	}
	return *remote.StockQuantity
}

func productStatus(status string) catalog.ProductStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "publish", "published", "active":
		return catalog.StatusActive
	case "private", "archived":
		return catalog.StatusArchived
	default:
		return catalog.StatusDraft
	}
}

func (e EventType) String() string {
	return string(e)
}

func (c Conflict) String() string {
	return fmt.Sprintf("%s:%s", c.SKU, c.Status)
}

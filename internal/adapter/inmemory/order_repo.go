package inmemory

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"
	orderdomain "github.com/nfsarch33/agentic-ecommerce/internal/domain/order"
)

var (
	ErrOrderNotFound = errors.New("order not found")
)

type OrderRepository struct {
	mu           sync.RWMutex
	orders       map[uuid.UUID]orderdomain.Order
	tenantOrders map[string]map[uuid.UUID]struct{}
}

func NewOrderRepository() *OrderRepository {
	return &OrderRepository{orders: make(map[uuid.UUID]orderdomain.Order), tenantOrders: make(map[string]map[uuid.UUID]struct{})}
}

func (r *OrderRepository) Create(_ context.Context, order orderdomain.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.orders[order.ID()] = order
	return nil
}

func (r *OrderRepository) CreateWithTenant(_ context.Context, order orderdomain.Order, tenantID string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ErrOrderNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tenantOrders == nil {
		r.tenantOrders = make(map[string]map[uuid.UUID]struct{})
	}
	r.orders[order.ID()] = order
	if r.tenantOrders[tenantID] == nil {
		r.tenantOrders[tenantID] = make(map[uuid.UUID]struct{})
	}
	r.tenantOrders[tenantID][order.ID()] = struct{}{}
	return nil
}

func (r *OrderRepository) GetByID(_ context.Context, id uuid.UUID) (orderdomain.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	order, ok := r.orders[id]
	if !ok {
		return orderdomain.Order{}, ErrOrderNotFound
	}
	return order, nil
}

func (r *OrderRepository) GetByIDAndTenant(_ context.Context, id uuid.UUID, tenantID string) (orderdomain.Order, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return orderdomain.Order{}, ErrOrderNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.tenantOrders[tenantID][id]; !ok {
		return orderdomain.Order{}, ErrOrderNotFound
	}
	order, ok := r.orders[id]
	if !ok {
		return orderdomain.Order{}, ErrOrderNotFound
	}
	return order, nil
}

func (r *OrderRepository) UpdateStatus(_ context.Context, id uuid.UUID, status orderdomain.Status) (orderdomain.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	order, ok := r.orders[id]
	if !ok {
		return orderdomain.Order{}, ErrOrderNotFound
	}
	if err := order.AdvanceStatus(status); err != nil {
		return orderdomain.Order{}, err
	}
	r.orders[id] = order
	return order, nil
}

func (r *OrderRepository) UpdateStatusWithTenant(_ context.Context, id uuid.UUID, status orderdomain.Status, tenantID string) (orderdomain.Order, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return orderdomain.Order{}, ErrOrderNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tenantOrders[tenantID][id]; !ok {
		return orderdomain.Order{}, ErrOrderNotFound
	}
	order, ok := r.orders[id]
	if !ok {
		return orderdomain.Order{}, ErrOrderNotFound
	}
	if err := order.AdvanceStatus(status); err != nil {
		return orderdomain.Order{}, err
	}
	r.orders[id] = order
	return order, nil
}

type CartRepository struct {
	mu    sync.RWMutex
	carts map[string]orderdomain.Cart
}

func NewCartRepository() *CartRepository {
	return &CartRepository{carts: make(map[string]orderdomain.Cart)}
}

func (r *CartRepository) Save(_ context.Context, cart orderdomain.Cart) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.carts[cart.SessionID()] = cart
	return nil
}

func (r *CartRepository) GetBySessionID(_ context.Context, sessionID string) (orderdomain.Cart, error) {
	r.mu.RLock()
	cart, ok := r.carts[sessionID]
	r.mu.RUnlock()
	if ok {
		return cart, nil
	}
	return orderdomain.NewCart(sessionID)
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	orderdomain "github.com/nfsarch33/agentic-ecommerce/internal/domain/order"
)

type totalsResponse struct {
	Subtotal moneyResponse `json:"subtotal"`
	Shipping moneyResponse `json:"shipping"`
	Total    moneyResponse `json:"total"`
}

type shippingAddressPayload struct {
	Name       string `json:"name"`
	Line1      string `json:"line1"`
	Line2      string `json:"line2,omitempty"`
	City       string `json:"city"`
	Region     string `json:"region,omitempty"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"`
}

type orderItemPayload struct {
	ProductID string        `json:"product_id"`
	SKU       string        `json:"sku"`
	Title     string        `json:"title"`
	Quantity  int           `json:"quantity"`
	UnitPrice moneyResponse `json:"unit_price"`
	LineTotal moneyResponse `json:"line_total,omitempty"`
}

type createOrderRequest struct {
	CustomerEmail   string                 `json:"customer_email"`
	Items           []orderItemPayload     `json:"items"`
	ShippingAddress shippingAddressPayload `json:"shipping_address"`
	Shipping        moneyResponse          `json:"shipping,omitempty"`
}

type orderResponse struct {
	ID              string                 `json:"id"`
	CustomerEmail   string                 `json:"customer_email"`
	Items           []orderItemPayload     `json:"items"`
	Status          string                 `json:"status"`
	Totals          totalsResponse         `json:"totals"`
	ShippingAddress shippingAddressPayload `json:"shipping_address"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

type updateOrderStatusRequest struct {
	Status string `json:"status"`
}

type cartRequest struct {
	Items []orderItemPayload `json:"items"`
}

type cartResponse struct {
	SessionID string             `json:"session_id"`
	Items     []orderItemPayload `json:"items"`
	Totals    totalsResponse     `json:"totals"`
	UpdatedAt time.Time          `json:"updated_at"`
}

func (s *server) ordersHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/orders"), "/")

	switch {
	case path == "" && r.Method == http.MethodPost:
		s.createOrder(w, r)
	case path != "" && !strings.Contains(path, "/") && r.Method == http.MethodGet:
		s.getOrder(w, r, path)
	case strings.HasSuffix(path, "/status") && r.Method == http.MethodPatch:
		id := strings.TrimSuffix(path, "/status")
		id = strings.TrimSuffix(id, "/")
		s.patchOrderStatus(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) createOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	input, err := req.toOrderInput()
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	order, err := orderdomain.NewOrder(input)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	var createErr error
	createScoped := false
	if tenantRepo, ok := s.orderRepo.(interface {
		CreateWithTenant(ctx context.Context, order orderdomain.Order, tenantID string) error
	}); ok {
		tenantID, scoped, tenantErr := s.tenantIDForScopedRequest(r)
		if tenantErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
			return
		}
		if scoped {
			createErr = tenantRepo.CreateWithTenant(r.Context(), order, string(tenantID))
			createScoped = true
		}
	}
	if !createScoped {
		createErr = s.orderRepo.Create(r.Context(), order)
	}
	if createErr != nil {
		s.log.Error("create order", "error", createErr)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	writeJSON(w, http.StatusCreated, toOrderResponse(order))
}

func (s *server) getOrder(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	var order orderdomain.Order
	readScoped := false
	if tenantRepo, ok := s.orderRepo.(interface {
		GetByIDAndTenant(ctx context.Context, id uuid.UUID, tenantID string) (orderdomain.Order, error)
	}); ok {
		tenantID, scoped, tenantErr := s.tenantIDForScopedRequest(r)
		if tenantErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
			return
		}
		if scoped {
			order, err = tenantRepo.GetByIDAndTenant(r.Context(), id, string(tenantID))
			readScoped = true
		}
	}
	if !readScoped {
		order, err = s.orderRepo.GetByID(r.Context(), id)
	}
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get order", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, toOrderResponse(order))
}

func (s *server) patchOrderStatus(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	var req updateOrderStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	status, err := orderdomain.ParseStatus(req.Status)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	var order orderdomain.Order
	updateScoped := false
	if tenantRepo, ok := s.orderRepo.(interface {
		UpdateStatusWithTenant(ctx context.Context, id uuid.UUID, status orderdomain.Status, tenantID string) (orderdomain.Order, error)
	}); ok {
		tenantID, scoped, tenantErr := s.tenantIDForScopedRequest(r)
		if tenantErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
			return
		}
		if scoped {
			order, err = tenantRepo.UpdateStatusWithTenant(r.Context(), id, status, string(tenantID))
			updateScoped = true
		}
	}
	if !updateScoped {
		order, err = s.orderRepo.UpdateStatus(r.Context(), id, status)
	}
	if err != nil {
		switch {
		case errors.Is(err, orderdomain.ErrInvalidStatusTransition):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		case isNotFound(err):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		default:
			s.log.Error("patch order status", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		}
		return
	}
	writeJSON(w, http.StatusOK, toOrderResponse(order))
}

func (s *server) cartHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/cart/"), "/")
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_session_id"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		cart, err := s.cartRepo.GetBySessionID(r.Context(), sessionID)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, toCartResponse(cart))
	case http.MethodPut:
		s.putCart(w, r, sessionID)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) putCart(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req cartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	cart, err := orderdomain.NewCart(sessionID)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	items, err := requestItemsToCartInputs(req.Items)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if err := cart.ReplaceItems(items); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if err := s.cartRepo.Save(r.Context(), cart); err != nil {
		s.log.Error("save cart", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, toCartResponse(cart))
}

func (req createOrderRequest) toOrderInput() (orderdomain.OrderInput, error) {
	items, err := requestItemsToOrderInputs(req.Items)
	if err != nil {
		return orderdomain.OrderInput{}, err
	}
	shipping := catalog.ZeroAUD()
	if req.Shipping.Currency != "" || req.Shipping.Amount != 0 {
		shipping, err = catalog.NewMoney(req.Shipping.Amount, req.Shipping.Currency)
		if err != nil {
			return orderdomain.OrderInput{}, err
		}
	}
	return orderdomain.OrderInput{
		CustomerEmail: req.CustomerEmail,
		Items:         items,
		ShippingAddress: orderdomain.ShippingAddress{
			Name:       req.ShippingAddress.Name,
			Line1:      req.ShippingAddress.Line1,
			Line2:      req.ShippingAddress.Line2,
			City:       req.ShippingAddress.City,
			Region:     req.ShippingAddress.Region,
			PostalCode: req.ShippingAddress.PostalCode,
			Country:    req.ShippingAddress.Country,
		},
		Shipping: shipping,
	}, nil
}

func requestItemsToOrderInputs(items []orderItemPayload) ([]orderdomain.OrderItemInput, error) {
	inputs := make([]orderdomain.OrderItemInput, 0, len(items))
	for _, item := range items {
		productID, err := uuid.Parse(item.ProductID)
		if err != nil {
			return nil, err
		}
		price, err := catalog.NewMoney(item.UnitPrice.Amount, item.UnitPrice.Currency)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, orderdomain.OrderItemInput{
			ProductID: productID,
			SKU:       item.SKU,
			Title:     item.Title,
			Quantity:  item.Quantity,
			UnitPrice: price,
		})
	}
	return inputs, nil
}

func requestItemsToCartInputs(items []orderItemPayload) ([]orderdomain.CartItemInput, error) {
	inputs := make([]orderdomain.CartItemInput, 0, len(items))
	for _, item := range items {
		productID, err := uuid.Parse(item.ProductID)
		if err != nil {
			return nil, err
		}
		price, err := catalog.NewMoney(item.UnitPrice.Amount, item.UnitPrice.Currency)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, orderdomain.CartItemInput{
			ProductID: productID,
			SKU:       item.SKU,
			Title:     item.Title,
			Quantity:  item.Quantity,
			UnitPrice: price,
		})
	}
	return inputs, nil
}

func toOrderResponse(order orderdomain.Order) orderResponse {
	items := make([]orderItemPayload, 0, len(order.Items()))
	for _, item := range order.Items() {
		items = append(items, toOrderItemPayload(item))
	}
	address := order.ShippingAddress()
	return orderResponse{
		ID:            order.ID().String(),
		CustomerEmail: order.CustomerEmail(),
		Items:         items,
		Status:        order.Status().String(),
		Totals:        toTotalsResponse(order.Totals()),
		ShippingAddress: shippingAddressPayload{
			Name:       address.Name,
			Line1:      address.Line1,
			Line2:      address.Line2,
			City:       address.City,
			Region:     address.Region,
			PostalCode: address.PostalCode,
			Country:    address.Country,
		},
		CreatedAt: order.CreatedAt(),
		UpdatedAt: order.UpdatedAt(),
	}
}

func toCartResponse(cart orderdomain.Cart) cartResponse {
	items := make([]orderItemPayload, 0, len(cart.Items()))
	for _, item := range cart.Items() {
		items = append(items, toCartItemPayload(item))
	}
	return cartResponse{SessionID: cart.SessionID(), Items: items, Totals: toTotalsResponse(cart.Totals()), UpdatedAt: cart.UpdatedAt()}
}

func toOrderItemPayload(item orderdomain.OrderItem) orderItemPayload {
	return orderItemPayload{
		ProductID: item.ProductID().String(),
		SKU:       item.SKU(),
		Title:     item.Title(),
		Quantity:  item.Quantity(),
		UnitPrice: moneyResponse{Amount: item.UnitPrice().Amount(), Currency: item.UnitPrice().Currency()},
		LineTotal: moneyResponse{Amount: item.LineTotal().Amount(), Currency: item.LineTotal().Currency()},
	}
}

func toCartItemPayload(item orderdomain.CartItem) orderItemPayload {
	return orderItemPayload{
		ProductID: item.ProductID().String(),
		SKU:       item.SKU(),
		Title:     item.Title(),
		Quantity:  item.Quantity(),
		UnitPrice: moneyResponse{Amount: item.UnitPrice().Amount(), Currency: item.UnitPrice().Currency()},
		LineTotal: moneyResponse{Amount: item.LineTotal().Amount(), Currency: item.LineTotal().Currency()},
	}
}

func toTotalsResponse(totals orderdomain.Totals) totalsResponse {
	return totalsResponse{
		Subtotal: moneyResponse{Amount: totals.Subtotal.Amount(), Currency: totals.Subtotal.Currency()},
		Shipping: moneyResponse{Amount: totals.Shipping.Amount(), Currency: totals.Shipping.Currency()},
		Total:    moneyResponse{Amount: totals.Total.Amount(), Currency: totals.Total.Currency()},
	}
}

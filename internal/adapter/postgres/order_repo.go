package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	orderdomain "github.com/nfsarch33/helixon-ec/internal/domain/order"
)

var (
	ErrOrderNotFound = errors.New("order not found")
	ErrCartNotFound  = errors.New("cart not found")
)

type OrderRepository struct {
	pool productStore
}

func NewOrderRepository(pool *pgxpool.Pool) *OrderRepository {
	return &OrderRepository{pool: pool}
}

func (r *OrderRepository) Create(ctx context.Context, order orderdomain.Order) error {
	address := order.ShippingAddress()
	totals := order.Totals()
	const orderSQL = `
		INSERT INTO orders (
			id, customer_email, status, subtotal_amount, currency, shipping_amount, total_amount,
			shipping_name, shipping_line1, shipping_line2, shipping_city, shipping_region,
			shipping_postal_code, shipping_country, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`
	_, err := r.pool.Exec(ctx, orderSQL,
		order.ID(),
		order.CustomerEmail(),
		order.Status().String(),
		totals.Subtotal.Amount(),
		totals.Total.Currency(),
		totals.Shipping.Amount(),
		totals.Total.Amount(),
		address.Name,
		address.Line1,
		address.Line2,
		address.City,
		address.Region,
		address.PostalCode,
		address.Country,
		order.CreatedAt(),
		order.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("insert order %s: %w", order.ID(), err)
	}

	for _, item := range order.Items() {
		if err := r.insertOrderItem(ctx, order.ID(), item); err != nil {
			return err
		}
	}
	return nil
}

func (r *OrderRepository) CreateWithTenant(ctx context.Context, order orderdomain.Order, tenantID string) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	address := order.ShippingAddress()
	totals := order.Totals()
	const orderSQL = `
		INSERT INTO orders (
			id, customer_email, status, subtotal_amount, currency, shipping_amount, total_amount,
			shipping_name, shipping_line1, shipping_line2, shipping_city, shipping_region,
			shipping_postal_code, shipping_country, tenant_id, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`
	_, err = r.pool.Exec(ctx, orderSQL,
		order.ID(),
		order.CustomerEmail(),
		order.Status().String(),
		totals.Subtotal.Amount(),
		totals.Total.Currency(),
		totals.Shipping.Amount(),
		totals.Total.Amount(),
		address.Name,
		address.Line1,
		address.Line2,
		address.City,
		address.Region,
		address.PostalCode,
		address.Country,
		tenantID,
		order.CreatedAt(),
		order.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("insert order %s (tenant %s): %w", order.ID(), tenantID, err)
	}

	for _, item := range order.Items() {
		if err := r.insertOrderItem(ctx, order.ID(), item); err != nil {
			return err
		}
	}
	return nil
}

func (r *OrderRepository) GetByID(ctx context.Context, id uuid.UUID) (orderdomain.Order, error) {
	order, err := r.getOrder(ctx, id)
	if err != nil {
		return orderdomain.Order{}, err
	}
	items, err := r.getOrderItems(ctx, id)
	if err != nil {
		return orderdomain.Order{}, err
	}
	return orderdomain.ReconstructOrder(orderdomain.OrderRecord{
		ID:              order.ID(),
		CustomerEmail:   order.CustomerEmail(),
		Items:           items,
		Status:          order.Status(),
		Totals:          order.Totals(),
		ShippingAddress: order.ShippingAddress(),
		CreatedAt:       order.CreatedAt(),
		UpdatedAt:       order.UpdatedAt(),
	}), nil
}

func (r *OrderRepository) GetByIDAndTenant(ctx context.Context, id uuid.UUID, tenantID string) (orderdomain.Order, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return orderdomain.Order{}, err
	}
	order, err := r.getOrderByTenant(ctx, id, tenantID)
	if err != nil {
		return orderdomain.Order{}, err
	}
	items, err := r.getOrderItems(ctx, id)
	if err != nil {
		return orderdomain.Order{}, err
	}
	return orderdomain.ReconstructOrder(orderdomain.OrderRecord{
		ID:              order.ID(),
		CustomerEmail:   order.CustomerEmail(),
		Items:           items,
		Status:          order.Status(),
		Totals:          order.Totals(),
		ShippingAddress: order.ShippingAddress(),
		CreatedAt:       order.CreatedAt(),
		UpdatedAt:       order.UpdatedAt(),
	}), nil
}

func (r *OrderRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status orderdomain.Status) (orderdomain.Order, error) {
	order, err := r.GetByID(ctx, id)
	if err != nil {
		return orderdomain.Order{}, err
	}
	if err := order.AdvanceStatus(status); err != nil {
		return orderdomain.Order{}, err
	}

	tag, err := r.pool.Exec(ctx, `UPDATE orders SET status = $2, updated_at = $3 WHERE id = $1`, id, status.String(), order.UpdatedAt())
	if err != nil {
		return orderdomain.Order{}, fmt.Errorf("update order status %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return orderdomain.Order{}, ErrOrderNotFound
	}
	return order, nil
}

func (r *OrderRepository) UpdateStatusWithTenant(ctx context.Context, id uuid.UUID, status orderdomain.Status, tenantID string) (orderdomain.Order, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return orderdomain.Order{}, err
	}
	order, err := r.GetByIDAndTenant(ctx, id, tenantID)
	if err != nil {
		return orderdomain.Order{}, err
	}
	if err := order.AdvanceStatus(status); err != nil {
		return orderdomain.Order{}, err
	}

	tag, err := r.pool.Exec(ctx, `UPDATE orders SET status = $3, updated_at = $4 WHERE id = $1 AND tenant_id = $2`, id, tenantID, status.String(), order.UpdatedAt())
	if err != nil {
		return orderdomain.Order{}, fmt.Errorf("update order status %s (tenant %s): %w", id, tenantID, err)
	}
	if tag.RowsAffected() == 0 {
		return orderdomain.Order{}, ErrOrderNotFound
	}
	return order, nil
}

func (r *OrderRepository) insertOrderItem(ctx context.Context, orderID uuid.UUID, item orderdomain.OrderItem) error {
	const q = `
		INSERT INTO order_items (
			order_id, product_id, sku, title, quantity, unit_price_amount, currency, line_total_amount
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := r.pool.Exec(ctx, q,
		orderID,
		item.ProductID(),
		item.SKU(),
		item.Title(),
		item.Quantity(),
		item.UnitPrice().Amount(),
		item.UnitPrice().Currency(),
		item.LineTotal().Amount(),
	)
	if err != nil {
		return fmt.Errorf("insert order item %s: %w", item.SKU(), err)
	}
	return nil
}

func (r *OrderRepository) getOrder(ctx context.Context, id uuid.UUID) (orderdomain.Order, error) {
	const q = `
		SELECT id, customer_email, status, subtotal_amount, currency, shipping_amount, total_amount,
		       shipping_name, shipping_line1, shipping_line2, shipping_city, shipping_region,
		       shipping_postal_code, shipping_country, created_at, updated_at
		FROM orders WHERE id = $1`
	order, err := scanOrderRow(r.pool.QueryRow(ctx, q, id), nil)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return orderdomain.Order{}, ErrOrderNotFound
		}
		return orderdomain.Order{}, err
	}
	return order, nil
}

func (r *OrderRepository) getOrderByTenant(ctx context.Context, id uuid.UUID, tenantID string) (orderdomain.Order, error) {
	const q = `
		SELECT id, customer_email, status, subtotal_amount, currency, shipping_amount, total_amount,
		       shipping_name, shipping_line1, shipping_line2, shipping_city, shipping_region,
		       shipping_postal_code, shipping_country, created_at, updated_at
		FROM orders WHERE id = $1 AND tenant_id = $2`
	order, err := scanOrderRow(r.pool.QueryRow(ctx, q, id, tenantID), nil)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return orderdomain.Order{}, ErrOrderNotFound
		}
		return orderdomain.Order{}, err
	}
	return order, nil
}

func (r *OrderRepository) getOrderItems(ctx context.Context, orderID uuid.UUID) ([]orderdomain.OrderItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT product_id, sku, title, quantity, unit_price_amount, currency, line_total_amount
		FROM order_items WHERE order_id = $1 ORDER BY id ASC`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list order items %s: %w", orderID, err)
	}
	defer rows.Close()

	var items []orderdomain.OrderItem
	for rows.Next() {
		item, err := scanOrderItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type CartRepository struct {
	pool productStore
}

func NewCartRepository(pool *pgxpool.Pool) *CartRepository {
	return &CartRepository{pool: pool}
}

func (r *CartRepository) Save(ctx context.Context, cart orderdomain.Cart) error {
	const cartSQL = `
		INSERT INTO carts (session_id, subtotal_amount, currency, total_amount, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (session_id) DO UPDATE
		SET subtotal_amount = EXCLUDED.subtotal_amount,
		    currency = EXCLUDED.currency,
		    total_amount = EXCLUDED.total_amount,
		    updated_at = EXCLUDED.updated_at`
	totals := cart.Totals()
	_, err := r.pool.Exec(ctx, cartSQL, cart.SessionID(), totals.Subtotal.Amount(), totals.Total.Currency(), totals.Total.Amount(), cart.UpdatedAt())
	if err != nil {
		return fmt.Errorf("upsert cart %s: %w", cart.SessionID(), err)
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM cart_items WHERE session_id = $1`, cart.SessionID()); err != nil {
		return fmt.Errorf("replace cart items %s: %w", cart.SessionID(), err)
	}
	for _, item := range cart.Items() {
		if err := r.insertCartItem(ctx, cart.SessionID(), item); err != nil {
			return err
		}
	}
	return nil
}

func (r *CartRepository) GetBySessionID(ctx context.Context, sessionID string) (orderdomain.Cart, error) {
	row := r.pool.QueryRow(ctx, `SELECT session_id, subtotal_amount, currency, total_amount, updated_at FROM carts WHERE session_id = $1`, sessionID)
	cart, err := scanCartRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return orderdomain.NewCart(sessionID)
		}
		return orderdomain.Cart{}, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT product_id, sku, title, quantity, unit_price_amount, currency, line_total_amount
		FROM cart_items WHERE session_id = $1 ORDER BY id ASC`, sessionID)
	if err != nil {
		return orderdomain.Cart{}, fmt.Errorf("list cart items %s: %w", sessionID, err)
	}
	defer rows.Close()

	var items []orderdomain.CartItem
	for rows.Next() {
		item, err := scanCartItem(rows)
		if err != nil {
			return orderdomain.Cart{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return orderdomain.Cart{}, err
	}
	return orderdomain.ReconstructCart(orderdomain.CartRecord{SessionID: cart.SessionID(), Items: items, Totals: cart.Totals(), UpdatedAt: cart.UpdatedAt()}), nil
}

func (r *CartRepository) insertCartItem(ctx context.Context, sessionID string, item orderdomain.CartItem) error {
	const q = `
		INSERT INTO cart_items (
			session_id, product_id, sku, title, quantity, unit_price_amount, currency, line_total_amount
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := r.pool.Exec(ctx, q,
		sessionID,
		item.ProductID(),
		item.SKU(),
		item.Title(),
		item.Quantity(),
		item.UnitPrice().Amount(),
		item.UnitPrice().Currency(),
		item.LineTotal().Amount(),
	)
	if err != nil {
		return fmt.Errorf("insert cart item %s: %w", item.SKU(), err)
	}
	return nil
}

func scanOrderRow(row pgx.Row, items []orderdomain.OrderItem) (orderdomain.Order, error) {
	var (
		id                             uuid.UUID
		email, status, currency        string
		subtotalAmount, shippingAmount int
		totalAmount                    int
		name, line1, line2, city       string
		region, postalCode, country    string
		createdAt, updatedAt           time.Time
	)
	err := row.Scan(&id, &email, &status, &subtotalAmount, &currency, &shippingAmount, &totalAmount, &name, &line1, &line2, &city, &region, &postalCode, &country, &createdAt, &updatedAt)
	if err != nil {
		return orderdomain.Order{}, err
	}
	parsedStatus, _ := orderdomain.ParseStatus(status)
	subtotal, _ := catalog.NewMoney(subtotalAmount, currency)
	shipping, _ := catalog.NewMoney(shippingAmount, currency)
	total, _ := catalog.NewMoney(totalAmount, currency)
	return orderdomain.ReconstructOrder(orderdomain.OrderRecord{
		ID:            id,
		CustomerEmail: email,
		Items:         items,
		Status:        parsedStatus,
		Totals:        orderdomain.Totals{Subtotal: subtotal, Shipping: shipping, Total: total},
		ShippingAddress: orderdomain.ShippingAddress{
			Name:       name,
			Line1:      line1,
			Line2:      line2,
			City:       city,
			Region:     region,
			PostalCode: postalCode,
			Country:    country,
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}), nil
}

func scanOrderItem(row scannable) (orderdomain.OrderItem, error) {
	var (
		productID                       uuid.UUID
		sku, title, currency            string
		quantity, unitAmount, lineTotal int
	)
	if err := row.Scan(&productID, &sku, &title, &quantity, &unitAmount, &currency, &lineTotal); err != nil {
		return orderdomain.OrderItem{}, err
	}
	unitPrice, _ := catalog.NewMoney(unitAmount, currency)
	total, _ := catalog.NewMoney(lineTotal, currency)
	return orderdomain.ReconstructOrderItem(orderdomain.OrderItemInput{
		ProductID: productID,
		SKU:       sku,
		Title:     title,
		Quantity:  quantity,
		UnitPrice: unitPrice,
	}, total), nil
}

func scanCartRow(row pgx.Row) (orderdomain.Cart, error) {
	var (
		sessionID                   string
		currency                    string
		subtotalAmount, totalAmount int
		updatedAt                   time.Time
	)
	if err := row.Scan(&sessionID, &subtotalAmount, &currency, &totalAmount, &updatedAt); err != nil {
		return orderdomain.Cart{}, err
	}
	subtotal, _ := catalog.NewMoney(subtotalAmount, currency)
	shipping, _ := catalog.NewMoney(0, currency)
	total, _ := catalog.NewMoney(totalAmount, currency)
	return orderdomain.ReconstructCart(orderdomain.CartRecord{
		SessionID: sessionID,
		Totals:    orderdomain.Totals{Subtotal: subtotal, Shipping: shipping, Total: total},
		UpdatedAt: updatedAt,
	}), nil
}

func scanCartItem(row scannable) (orderdomain.CartItem, error) {
	var (
		productID                       uuid.UUID
		sku, title, currency            string
		quantity, unitAmount, lineTotal int
	)
	if err := row.Scan(&productID, &sku, &title, &quantity, &unitAmount, &currency, &lineTotal); err != nil {
		return orderdomain.CartItem{}, err
	}
	unitPrice, _ := catalog.NewMoney(unitAmount, currency)
	total, _ := catalog.NewMoney(lineTotal, currency)
	return orderdomain.ReconstructCartItem(orderdomain.CartItemInput{
		ProductID: productID,
		SKU:       sku,
		Title:     title,
		Quantity:  quantity,
		UnitPrice: unitPrice,
	}, total), nil
}

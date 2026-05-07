package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

var ErrProductNotFound = errors.New("product not found")

type ProductRepository struct {
	pool productStore
}

func NewProductRepository(pool *pgxpool.Pool) *ProductRepository {
	return &ProductRepository{pool: pool}
}

type productStore interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (r *ProductRepository) Create(ctx context.Context, product catalog.Product) error {
	const q = `
		INSERT INTO products (id, sku, title, slug, description, price_amount, price_currency, stock, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

	_, err := r.pool.Exec(ctx, q,
		product.ID(),
		product.SKU(),
		product.Title(),
		product.Slug(),
		product.Description(),
		product.Price().Amount(),
		product.Price().Currency(),
		product.Stock(),
		product.Status().String(),
		product.CreatedAt(),
		product.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("insert product %s: %w", product.SKU(), err)
	}
	return nil
}

func (r *ProductRepository) GetByID(ctx context.Context, id uuid.UUID) (catalog.Product, error) {
	return r.getOne(ctx, `SELECT id, sku, title, slug, description, price_amount, price_currency, stock, status, created_at, updated_at FROM products WHERE id = $1`, id)
}

func (r *ProductRepository) GetBySlug(ctx context.Context, slug string) (catalog.Product, error) {
	return r.getOne(ctx, `SELECT id, sku, title, slug, description, price_amount, price_currency, stock, status, created_at, updated_at FROM products WHERE slug = $1`, slug)
}

func (r *ProductRepository) List(ctx context.Context, page, perPage int) (port.ListResult, error) {
	var total int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM products`).Scan(&total)
	if err != nil {
		return port.ListResult{}, fmt.Errorf("count products: %w", err)
	}

	offset := (page - 1) * perPage
	rows, err := r.pool.Query(ctx,
		`SELECT id, sku, title, slug, description, price_amount, price_currency, stock, status, created_at, updated_at
		 FROM products ORDER BY created_at ASC LIMIT $1 OFFSET $2`, perPage, offset)
	if err != nil {
		return port.ListResult{}, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	var products []catalog.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return port.ListResult{}, err
		}
		products = append(products, p)
	}

	return port.ListResult{Products: products, Total: total}, rows.Err()
}

func (r *ProductRepository) Update(ctx context.Context, product catalog.Product) error {
	const q = `
		UPDATE products
		SET sku = $2, title = $3, slug = $4, description = $5,
		    price_amount = $6, price_currency = $7, stock = $8, status = $9, updated_at = $10
		WHERE id = $1`

	tag, err := r.pool.Exec(ctx, q,
		product.ID(),
		product.SKU(),
		product.Title(),
		product.Slug(),
		product.Description(),
		product.Price().Amount(),
		product.Price().Currency(),
		product.Stock(),
		product.Status().String(),
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("update product %s: %w", product.SKU(), err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProductNotFound
	}
	return nil
}

func (r *ProductRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProductNotFound
	}
	return nil
}

func (r *ProductRepository) CreateWithTenant(ctx context.Context, product catalog.Product, tenantID string) error {
	const q = `
		INSERT INTO products (id, sku, title, slug, description, price_amount, price_currency, stock, status, tenant_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

	_, err := r.pool.Exec(ctx, q,
		product.ID(),
		product.SKU(),
		product.Title(),
		product.Slug(),
		product.Description(),
		product.Price().Amount(),
		product.Price().Currency(),
		product.Stock(),
		product.Status().String(),
		tenantID,
		product.CreatedAt(),
		product.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("insert product %s (tenant %s): %w", product.SKU(), tenantID, err)
	}
	return nil
}

func (r *ProductRepository) ListByTenant(ctx context.Context, tenantID string, page, perPage int) (port.ListResult, error) {
	var total int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM products WHERE tenant_id = $1`, tenantID).Scan(&total)
	if err != nil {
		return port.ListResult{}, fmt.Errorf("count products by tenant: %w", err)
	}

	offset := (page - 1) * perPage
	rows, err := r.pool.Query(ctx,
		`SELECT id, sku, title, slug, description, price_amount, price_currency, stock, status, created_at, updated_at
		 FROM products WHERE tenant_id = $1 ORDER BY created_at ASC LIMIT $2 OFFSET $3`, tenantID, perPage, offset)
	if err != nil {
		return port.ListResult{}, fmt.Errorf("list products by tenant: %w", err)
	}
	defer rows.Close()

	var products []catalog.Product
	for rows.Next() {
		p, scanErr := scanProduct(rows)
		if scanErr != nil {
			return port.ListResult{}, scanErr
		}
		products = append(products, p)
	}

	return port.ListResult{Products: products, Total: total}, rows.Err()
}

func (r *ProductRepository) GetByIDAndTenant(ctx context.Context, id uuid.UUID, tenantID string) (catalog.Product, error) {
	const q = `SELECT id, sku, title, slug, description, price_amount, price_currency, stock, status, created_at, updated_at
		FROM products WHERE id = $1 AND tenant_id = $2`
	row := r.pool.QueryRow(ctx, q, id, tenantID)
	p, err := scanProductRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.Product{}, ErrProductNotFound
		}
		return catalog.Product{}, err
	}
	return p, nil
}

func (r *ProductRepository) getOne(ctx context.Context, query string, arg any) (catalog.Product, error) {
	row := r.pool.QueryRow(ctx, query, arg)
	p, err := scanProductRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return catalog.Product{}, ErrProductNotFound
		}
		return catalog.Product{}, err
	}
	return p, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanProductRow(row pgx.Row) (catalog.Product, error) {
	var (
		id            uuid.UUID
		sku, title    string
		slug, desc    string
		priceAmount   int
		priceCurrency string
		stock         int
		status        string
		createdAt     time.Time
		updatedAt     time.Time
	)
	err := row.Scan(&id, &sku, &title, &slug, &desc, &priceAmount, &priceCurrency, &stock, &status, &createdAt, &updatedAt)
	if err != nil {
		return catalog.Product{}, err
	}
	price, _ := catalog.NewMoney(priceAmount, priceCurrency)
	parsedStatus, _ := catalog.ParseProductStatus(status)

	return catalog.ReconstructProduct(catalog.ProductRecord{
		ID:          id,
		SKU:         sku,
		Title:       title,
		Slug:        slug,
		Description: desc,
		Price:       price,
		Stock:       stock,
		Status:      parsedStatus,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}), nil
}

func scanProduct(rows pgx.Rows) (catalog.Product, error) {
	var (
		id            uuid.UUID
		sku, title    string
		slug, desc    string
		priceAmount   int
		priceCurrency string
		stock         int
		status        string
		createdAt     time.Time
		updatedAt     time.Time
	)
	err := rows.Scan(&id, &sku, &title, &slug, &desc, &priceAmount, &priceCurrency, &stock, &status, &createdAt, &updatedAt)
	if err != nil {
		return catalog.Product{}, err
	}
	price, _ := catalog.NewMoney(priceAmount, priceCurrency)
	parsedStatus, _ := catalog.ParseProductStatus(status)

	return catalog.ReconstructProduct(catalog.ProductRecord{
		ID:          id,
		SKU:         sku,
		Title:       title,
		Slug:        slug,
		Description: desc,
		Price:       price,
		Stock:       stock,
		Status:      parsedStatus,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}), nil
}

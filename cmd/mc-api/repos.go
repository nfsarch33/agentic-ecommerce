package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/postgres"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// repos.go (v2.6.1 cmd/* DI refactor): tiny constructors that wrap
// the in-memory repository wiring main() previously inlined. Pulled
// out so mainImpl in app.go can call them without redeclaring the
// imports and so future tests can shadow the seeding behaviour.

func newSeededProductRepository() *inmemory.ProductRepository {
	repo := inmemory.NewProductRepository()
	seedDefaultProducts(repo)
	return repo
}

func newOrderAndCartRepos() (*inmemory.OrderRepository, *inmemory.CartRepository) {
	return inmemory.NewOrderRepository(), inmemory.NewCartRepository()
}

// newProductRepositoryFromEnv returns the PG-backed product repository when
// ECOMMERCE_DB_URL is set (the temporal-worker's pattern), and the seeded
// in-memory repository otherwise (local dev, tests). The API and the worker
// must agree on the product store: a workflow the API starts on a product
// the worker cannot see fails its compliance check with exactly the
// not-found the fixture hit on 4 Oct.
func newProductRepositoryFromEnv(getenv func(string) string) (port.ProductRepository, func()) {
	dsn := strings.TrimSpace(getenv("ECOMMERCE_DB_URL"))
	if dsn == "" {
		return newSeededProductRepository(), nil
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		// A DSN that cannot even build a pool is a configuration error, not
		// a degrade-to-memory: silently serving seeded demo products while
		// the worker reads PG would replay the not-found split.
		panic(fmt.Sprintf("mc-api: ECOMMERCE_DB_URL is set but unusable: %v", err))
	}
	return postgres.NewProductRepository(pool), pool.Close
}

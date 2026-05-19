package main

import (
	"github.com/nfsarch33/helixon-ec/internal/adapter/inmemory"
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

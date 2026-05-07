package catalog

import (
	"context"
	"errors"
	"testing"

	domain "github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

type recordingChannel struct {
	products []domain.Product
	err      error
}

func (c *recordingChannel) UpsertProduct(ctx context.Context, product domain.Product) error {
	if c.err != nil {
		return c.err
	}
	c.products = append(c.products, product)
	return nil
}

func TestSyncProduct_UpsertsValidatedProduct(t *testing.T) {
	t.Parallel()

	channel := &recordingChannel{}
	service := NewSyncService(channel)

	product, err := service.SyncProduct(context.Background(), domain.ProductInput{
		SKU:   " band-001 ",
		Title: "Resistance Band",
	})
	if err != nil {
		t.Fatalf("sync product: %v", err)
	}

	if product.SKU() != "BAND-001" {
		t.Fatalf("SKU() = %q, want BAND-001", product.SKU())
	}
	if len(channel.products) != 1 {
		t.Fatalf("upserts = %d, want 1", len(channel.products))
	}
}

func TestSyncProduct_PropagatesChannelFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("woocommerce unavailable")
	service := NewSyncService(&recordingChannel{err: want})

	_, err := service.SyncProduct(context.Background(), domain.ProductInput{
		SKU:   "BAND-001",
		Title: "Resistance Band",
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

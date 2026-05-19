package sync

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/adapter/inmemory"
	"github.com/nfsarch33/helixon-ec/internal/adapter/woocommerce"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
)

type fakeWooCommerce struct {
	products []woocommerce.Product
	upserts  []catalog.Product
	err      error
}

func (f *fakeWooCommerce) ListProducts(context.Context, woocommerce.ListOptions) ([]woocommerce.Product, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.products, nil
}

func (f *fakeWooCommerce) UpsertProduct(_ context.Context, p catalog.Product) error {
	if f.err != nil {
		return f.err
	}
	f.upserts = append(f.upserts, p)
	return nil
}

func TestImportFromWooCommerceCreatesLocalProductsAndRecordsEvent(t *testing.T) {
	t.Parallel()

	repo := inmemory.NewProductRepository()
	wc := &fakeWooCommerce{products: []woocommerce.Product{{
		ID: 7, SKU: "wc-001", Name: "Woo Band", Regular: "19.95", Description: "Imported from Woo", StockQuantity: intPtr(4),
	}}}
	engine := NewEngine(Config{ProductRepository: repo, WooCommerce: wc, DefaultCurrency: "AUD"})

	result, err := engine.ImportFromWooCommerce(context.Background(), ImportOptions{PerPage: 50})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Imported != 1 || result.Conflicts != 0 {
		t.Fatalf("result = %+v, want 1 import and 0 conflicts", result)
	}

	list, err := repo.List(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if len(list.Products) != 1 || list.Products[0].SKU() != "WC-001" {
		t.Fatalf("products = %+v", list.Products)
	}

	events := engine.Events()
	if len(events) != 1 || events[0].Type != EventProductImported {
		t.Fatalf("events = %+v", events)
	}
}

func TestImportFromWooCommerceIsIdempotentForUnchangedRemoteProducts(t *testing.T) {
	t.Parallel()

	repo := inmemory.NewProductRepository()
	wc := &fakeWooCommerce{products: []woocommerce.Product{{
		ID: 7, SKU: " wc-001 ", Name: "Woo Band", Regular: "19.95", Description: "Imported from Woo", StockQuantity: intPtr(4),
	}}}
	engine := NewEngine(Config{ProductRepository: repo, WooCommerce: wc, DefaultCurrency: "AUD"})

	first, err := engine.ImportFromWooCommerce(context.Background(), ImportOptions{PerPage: 50})
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	second, err := engine.ImportFromWooCommerce(context.Background(), ImportOptions{PerPage: 50})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if first.Imported != 1 || second.Imported != 0 || second.Conflicts != 0 {
		t.Fatalf("first=%+v second=%+v, want initial import only", first, second)
	}
	list, err := repo.List(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if len(list.Products) != 1 || list.Products[0].Title() != "Woo Band" {
		t.Fatalf("products = %+v", list.Products)
	}
	if events := engine.Events(); len(events) != 1 || events[0].Type != EventProductImported {
		t.Fatalf("events = %+v, want one import event", events)
	}
}

func TestImportFromWooCommerceDetectsDivergentProductConflict(t *testing.T) {
	t.Parallel()

	repo := inmemory.NewProductRepository()
	local := mustProduct(t, "SKU-1", "Local title", "Local description", 1000, 3)
	if err := repo.Create(context.Background(), local); err != nil {
		t.Fatalf("create local: %v", err)
	}
	wc := &fakeWooCommerce{products: []woocommerce.Product{{
		ID: 11, SKU: "SKU-1", Name: "Remote title", Regular: "12.50", Description: "Remote description", StockQuantity: intPtr(8),
	}}}
	engine := NewEngine(Config{ProductRepository: repo, WooCommerce: wc, DefaultCurrency: "AUD"})

	result, err := engine.ImportFromWooCommerce(context.Background(), ImportOptions{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Imported != 0 || result.Conflicts != 1 {
		t.Fatalf("result = %+v, want conflict only", result)
	}

	conflicts := engine.Conflicts()
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	wantFields := map[string]bool{"title": true, "price": true, "stock": true, "description": true}
	for _, field := range conflicts[0].Fields {
		delete(wantFields, field.Field)
	}
	if len(wantFields) != 0 {
		t.Fatalf("missing conflict fields: %+v in %+v", wantFields, conflicts[0].Fields)
	}

	events := engine.Events()
	if len(events) != 1 || events[0].Type != EventConflictDetected {
		t.Fatalf("events = %+v", events)
	}
}

func TestImportFromWooCommerceDoesNotDuplicateConflictRecords(t *testing.T) {
	t.Parallel()

	repo := inmemory.NewProductRepository()
	local := mustProduct(t, "SKU-1", "Local title", "Local description", 1000, 3)
	if err := repo.Create(context.Background(), local); err != nil {
		t.Fatalf("create local: %v", err)
	}
	wc := &fakeWooCommerce{products: []woocommerce.Product{{
		ID: 11, SKU: "SKU-1", Name: "Remote title", Regular: "12.50", Description: "Remote description", StockQuantity: intPtr(8),
	}}}
	engine := NewEngine(Config{ProductRepository: repo, WooCommerce: wc, DefaultCurrency: "AUD"})

	if _, err := engine.ImportFromWooCommerce(context.Background(), ImportOptions{}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if _, err := engine.ImportFromWooCommerce(context.Background(), ImportOptions{}); err != nil {
		t.Fatalf("second import: %v", err)
	}

	conflicts := engine.Conflicts()
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want one deterministic conflict", conflicts)
	}
	if conflicts[0].ID == "" || conflicts[0].ProductID != local.ID().String() || conflicts[0].SKU != "SKU-1" || conflicts[0].RemoteID != 11 {
		t.Fatalf("conflict identity = %+v", conflicts[0])
	}
	wantFields := []ConflictField{
		{Field: "title", LocalValue: "Local title", RemoteValue: "Remote title"},
		{Field: "price", LocalValue: "1000", RemoteValue: "1250"},
		{Field: "stock", LocalValue: "3", RemoteValue: "8"},
		{Field: "description", LocalValue: "Local description", RemoteValue: "Remote description"},
	}
	if len(conflicts[0].Fields) != len(wantFields) {
		t.Fatalf("fields = %+v, want %+v", conflicts[0].Fields, wantFields)
	}
	for idx, want := range wantFields {
		if conflicts[0].Fields[idx] != want {
			t.Fatalf("field[%d] = %+v, want %+v", idx, conflicts[0].Fields[idx], want)
		}
	}
	if events := engine.Events(); len(events) != 1 || events[0].Type != EventConflictDetected {
		t.Fatalf("events = %+v, want one conflict event", events)
	}
}

func TestPublishToWooCommerceRecordsPublishedOrFailedEvent(t *testing.T) {
	t.Parallel()

	repo := inmemory.NewProductRepository()
	product := mustProduct(t, "SKU-2", "Publish me", "Ready", 2500, 9)
	if err := repo.Create(context.Background(), product); err != nil {
		t.Fatalf("create product: %v", err)
	}
	wc := &fakeWooCommerce{}
	engine := NewEngine(Config{ProductRepository: repo, WooCommerce: wc, DefaultCurrency: "AUD"})

	if err := engine.PublishToWooCommerce(context.Background(), product.ID()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(wc.upserts) != 1 || wc.upserts[0].ID() != product.ID() {
		t.Fatalf("upserts = %+v", wc.upserts)
	}
	if got := engine.Events()[0].Type; got != EventProductPublished {
		t.Fatalf("event = %s, want %s", got, EventProductPublished)
	}

	wc.err = errors.New("woocommerce down")
	if err := engine.PublishToWooCommerce(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected publish error")
	}
	events := engine.Events()
	if got := events[len(events)-1].Type; got != EventSyncFailed {
		t.Fatalf("last event = %s, want %s", got, EventSyncFailed)
	}
}

func TestPublishToWooCommerceIsIdempotentForUnchangedProducts(t *testing.T) {
	t.Parallel()

	repo := inmemory.NewProductRepository()
	product := mustProduct(t, "SKU-2", "Publish me", "Ready", 2500, 9)
	if err := repo.Create(context.Background(), product); err != nil {
		t.Fatalf("create product: %v", err)
	}
	wc := &fakeWooCommerce{}
	engine := NewEngine(Config{ProductRepository: repo, WooCommerce: wc, DefaultCurrency: "AUD"})

	if err := engine.PublishToWooCommerce(context.Background(), product.ID()); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if err := engine.PublishToWooCommerce(context.Background(), product.ID()); err != nil {
		t.Fatalf("second publish: %v", err)
	}

	if len(wc.upserts) != 1 {
		t.Fatalf("upserts = %d, want one unchanged publish", len(wc.upserts))
	}
	if events := engine.Events(); len(events) != 1 || events[0].Type != EventProductPublished {
		t.Fatalf("events = %+v, want one publish event", events)
	}
}

func TestResolveConflictMarksManualDecision(t *testing.T) {
	t.Parallel()

	repo := inmemory.NewProductRepository()
	local := mustProduct(t, "SKU-3", "Local", "Local", 1000, 1)
	if err := repo.Create(context.Background(), local); err != nil {
		t.Fatalf("create local: %v", err)
	}
	wc := &fakeWooCommerce{products: []woocommerce.Product{{ID: 12, SKU: "SKU-3", Name: "Remote", Regular: "10.00"}}}
	engine := NewEngine(Config{ProductRepository: repo, WooCommerce: wc, DefaultCurrency: "AUD"})
	if _, err := engine.ImportFromWooCommerce(context.Background(), ImportOptions{}); err != nil {
		t.Fatalf("import: %v", err)
	}

	conflictID := engine.Conflicts()[0].ID
	resolved, err := engine.ResolveConflict(conflictID, "local", "keep local catalog copy")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != ConflictResolved || resolved.Resolution != "local" {
		t.Fatalf("resolved conflict = %+v", resolved)
	}
	if pending := engine.Conflicts(); len(pending) != 0 {
		t.Fatalf("pending conflicts = %+v", pending)
	}
}

func TestResolveConflictAcceptsLocalRemoteAndManualDecisions(t *testing.T) {
	t.Parallel()

	for _, resolution := range []string{"local", "remote", "manual"} {
		t.Run(resolution, func(t *testing.T) {
			t.Parallel()
			repo := inmemory.NewProductRepository()
			local := mustProduct(t, "SKU-"+resolution, "Local", "Local", 1000, 1)
			if err := repo.Create(context.Background(), local); err != nil {
				t.Fatalf("create local: %v", err)
			}
			wc := &fakeWooCommerce{products: []woocommerce.Product{{ID: 12, SKU: local.SKU(), Name: "Remote", Regular: "10.00"}}}
			engine := NewEngine(Config{ProductRepository: repo, WooCommerce: wc, DefaultCurrency: "AUD"})
			if _, err := engine.ImportFromWooCommerce(context.Background(), ImportOptions{}); err != nil {
				t.Fatalf("import: %v", err)
			}

			resolved, err := engine.ResolveConflict(engine.Conflicts()[0].ID, resolution, "operator decision")
			if err != nil {
				t.Fatalf("resolve %q: %v", resolution, err)
			}
			if resolved.Status != ConflictResolved || resolved.Resolution != resolution {
				t.Fatalf("resolved = %+v", resolved)
			}
		})
	}
}

func TestReconcileInventoryRecordsSummaryEventAndStatus(t *testing.T) {
	t.Parallel()

	repo := inmemory.NewProductRepository()
	wc := &fakeWooCommerce{products: []woocommerce.Product{{ID: 21, SKU: "REC-1", Name: "Reconcile", Regular: "7.50", StockQuantity: intPtr(6)}}}
	engine := NewEngine(Config{ProductRepository: repo, WooCommerce: wc, DefaultCurrency: "AUD"})

	result, err := engine.ReconcileInventory(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Imported != 1 {
		t.Fatalf("result = %+v", result)
	}
	events := engine.Events()
	if len(events) != 2 || events[1].Type != EventInventoryReconciled {
		t.Fatalf("events = %+v", events)
	}
	status := engine.Status()
	if status.TotalEvents != 2 || status.PendingConflicts != 0 || status.LastEvent == nil {
		t.Fatalf("status = %+v", status)
	}
}

func TestReconcileInventoryIsIdempotentForUnchangedRemoteProducts(t *testing.T) {
	t.Parallel()

	repo := inmemory.NewProductRepository()
	wc := &fakeWooCommerce{products: []woocommerce.Product{{ID: 21, SKU: "REC-1", Name: "Reconcile", Regular: "7.50", StockQuantity: intPtr(6)}}}
	engine := NewEngine(Config{ProductRepository: repo, WooCommerce: wc, DefaultCurrency: "AUD"})

	if _, err := engine.ReconcileInventory(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if _, err := engine.ReconcileInventory(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	events := engine.Events()
	if len(events) != 2 || events[0].Type != EventProductImported || events[1].Type != EventInventoryReconciled {
		t.Fatalf("events = %+v, want one import and one reconcile event", events)
	}
}

func TestRecordEventAddsExternalWebhookEvent(t *testing.T) {
	t.Parallel()

	engine := NewEngine(Config{ProductRepository: inmemory.NewProductRepository(), WooCommerce: &fakeWooCommerce{}})
	engine.RecordEvent(Event{Type: EventInventoryReconciled, RemoteID: 99, Message: "webhook accepted"})

	events := engine.Events()
	if len(events) != 1 || events[0].ID == "" || events[0].CreatedAt.IsZero() {
		t.Fatalf("events = %+v", events)
	}
	status := engine.Status()
	if status.LastEvent == nil || status.LastEvent.RemoteID != 99 {
		t.Fatalf("status = %+v", status)
	}
}

func mustProduct(t *testing.T, sku, title, description string, amount, stock int) catalog.Product {
	t.Helper()
	price, err := catalog.NewMoney(amount, "AUD")
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	product, err := catalog.NewProduct(catalog.ProductInput{
		SKU:         sku,
		Title:       title,
		Description: description,
		Price:       price,
		Stock:       stock,
		Status:      catalog.StatusActive,
	})
	if err != nil {
		t.Fatalf("product: %v", err)
	}
	return product
}

func intPtr(v int) *int { return &v }

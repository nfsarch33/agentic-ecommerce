// File scope: v6.2.0 CF-13 unit tests for the Postgres-backed
// customerservice.FAQStore adapter.
//
// Real-Postgres coverage lives behind the `integration_pg` build
// tag in faq_store_integration_test.go (added in QA pair v6.2.1);
// this file exercises the adapter through a fake productStore so
// the default `go test ./...` lane stays hermetic.
package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/customerservice"
)

type fakeFAQRow struct {
	tenant   string
	entryID  string
	language string
	intent   string
	question string
	answer   string
	keywords []string
}

type fakeFAQRows struct {
	idx     int
	rows    []fakeFAQRow
	closed  bool
	scanErr error
	iterErr error
}

func (r *fakeFAQRows) Next() bool {
	if r.iterErr != nil {
		return false
	}
	r.idx++
	return r.idx <= len(r.rows)
}

func (r *fakeFAQRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if r.idx <= 0 || r.idx > len(r.rows) {
		return errors.New("scan called with no current row")
	}
	if len(dest) != 7 {
		return errors.New("expected 7 dest cols")
	}
	src := r.rows[r.idx-1]
	*(dest[0].(*string)) = src.tenant
	*(dest[1].(*string)) = src.entryID
	*(dest[2].(*string)) = src.language
	*(dest[3].(*string)) = src.intent
	*(dest[4].(*string)) = src.question
	*(dest[5].(*string)) = src.answer
	*(dest[6].(*[]string)) = append([]string{}, src.keywords...)
	return nil
}

func (r *fakeFAQRows) Close()                                       { r.closed = true }
func (r *fakeFAQRows) Err() error                                   { return r.iterErr }
func (r *fakeFAQRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeFAQRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeFAQRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeFAQRows) RawValues() [][]byte                          { return nil }
func (r *fakeFAQRows) Conn() *pgx.Conn                              { return nil }

type fakeFAQPool struct {
	rows     []fakeFAQRow
	queryErr error
	lastSQL  string
	lastArgs []any
}

func (p *fakeFAQPool) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("not used")
}

func (p *fakeFAQPool) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	p.lastSQL = sql
	p.lastArgs = args
	if p.queryErr != nil {
		return nil, p.queryErr
	}
	return &fakeFAQRows{rows: p.rows}, nil
}

func (p *fakeFAQPool) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return &fakeIdempotencyRow{err: errors.New("not used")}
}

func TestFAQStore_RejectsEmptyTenant(t *testing.T) {
	t.Parallel()
	store := &FAQStore{pool: &fakeFAQPool{}}
	_, err := store.Search(context.Background(), customerservice.FAQSearchQuery{})
	if err == nil {
		t.Fatal("Search empty tenant: nil err, want validation error")
	}
}

func TestFAQStore_ReturnsScopedEntries(t *testing.T) {
	t.Parallel()
	pool := &fakeFAQPool{rows: []fakeFAQRow{
		{tenant: "tenant-cs", entryID: "faq-1", language: "en", intent: "shipping_query", question: "shipping?", answer: "5-10 days", keywords: []string{"shipping"}},
		{tenant: "tenant-cs", entryID: "faq-2", language: "en", intent: "shipping_query", question: "sydney?", answer: "3-5 days", keywords: []string{"sydney"}},
	}}
	store := &FAQStore{pool: pool}
	got, err := store.Search(context.Background(), customerservice.FAQSearchQuery{
		TenantID: "tenant-cs",
		Language: customerservice.LanguageEN,
		Intent:   customerservice.IntentShippingQuery,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d want 2", len(got))
	}
	if got[0].TenantID != "tenant-cs" || got[1].EntryID != "faq-2" {
		t.Fatalf("rows[0]=%+v rows[1]=%+v", got[0], got[1])
	}
	if !strings.Contains(pool.lastSQL, "tenant_id = $1") {
		t.Fatalf("missing tenant scope in SQL: %s", pool.lastSQL)
	}
	if !strings.Contains(pool.lastSQL, "language = $2") {
		t.Fatalf("missing language scope in SQL: %s", pool.lastSQL)
	}
	if !strings.Contains(pool.lastSQL, "intent_category = $3") {
		t.Fatalf("missing intent scope in SQL: %s", pool.lastSQL)
	}
	if pool.lastArgs[0] != "tenant-cs" || pool.lastArgs[1] != "en" || pool.lastArgs[2] != "shipping_query" {
		t.Fatalf("args=%v", pool.lastArgs)
	}
}

func TestFAQStore_LooseFilterDropsLanguage(t *testing.T) {
	t.Parallel()
	pool := &fakeFAQPool{rows: []fakeFAQRow{
		{tenant: "tenant-cs", entryID: "faq-9", language: "en", intent: "general_enquiry", question: "hours?", answer: "24/7"},
	}}
	store := &FAQStore{pool: pool}
	if _, err := store.Search(context.Background(), customerservice.FAQSearchQuery{TenantID: "tenant-cs"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if strings.Contains(pool.lastSQL, "language = ") {
		t.Fatalf("loose filter still scoping language: %s", pool.lastSQL)
	}
	if strings.Contains(pool.lastSQL, "intent_category = ") {
		t.Fatalf("loose filter still scoping intent: %s", pool.lastSQL)
	}
}

func TestFAQStore_QueryErrorWraps(t *testing.T) {
	t.Parallel()
	pool := &fakeFAQPool{queryErr: errors.New("connection reset")}
	store := &FAQStore{pool: pool}
	_, err := store.Search(context.Background(), customerservice.FAQSearchQuery{TenantID: "tenant-cs"})
	if err == nil {
		t.Fatal("Search: expected wrapped error")
	}
	if !strings.Contains(err.Error(), "faq_entries query") {
		t.Fatalf("err missing wrap context: %v", err)
	}
}

func TestFAQStore_ImplementsFAQStorePort(t *testing.T) {
	t.Parallel()
	var _ customerservice.FAQStore = (*FAQStore)(nil)
}

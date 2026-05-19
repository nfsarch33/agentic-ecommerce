// File scope: v6.2.0 CF-13 Postgres adapter for the
// customerservice.FAQStore port.
//
// Closes the long-standing carry-forward where the v3.6.0 EC-8-2
// responder shipped with only an in-memory adapter. Migration
// 0039_faq_store.sql adds the supporting indexes; the underlying
// faq_entries table itself comes from migration 0015 (v3.6.0).
//
// Coupling: the import points adapter -> domain (customerservice),
// preserving the Clean Architecture seam.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nfsarch33/helixon-ec/internal/agent/customerservice"
)

// FAQStore is the Postgres-backed implementation of
// customerservice.FAQStore. Goroutine-safe via pgxpool.
type FAQStore struct {
	pool productStore
}

// NewFAQStore wires the adapter to a pgx pool.
func NewFAQStore(pool *pgxpool.Pool) *FAQStore {
	return &FAQStore{pool: pool}
}

// Search returns the FAQ entries scoped to the (tenant, language,
// intent) tuple. Empty Language or Intent loosen the filter so
// callers can fall back to a tenant-only sweep.
//
// Cyclomatic 4.
func (s *FAQStore) Search(ctx context.Context, query customerservice.FAQSearchQuery) ([]customerservice.FAQEntry, error) {
	if query.TenantID == "" {
		return nil, errors.New("postgres.FAQStore.Search: tenant_id required")
	}
	q, args := buildFAQSearch(query)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("faq_entries query: %w", err)
	}
	defer rows.Close()
	out := make([]customerservice.FAQEntry, 0, 16)
	for rows.Next() {
		entry, scanErr := scanFAQRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("faq_entries iterate: %w", err)
	}
	return out, nil
}

func buildFAQSearch(q customerservice.FAQSearchQuery) (string, []any) {
	const base = `
		SELECT tenant_id, entry_id::text, language, intent_category,
		       question, answer, COALESCE(keywords, '{}')::text[]
		FROM faq_entries
		WHERE tenant_id = $1`
	args := []any{q.TenantID}
	sql := base
	if q.Language != "" {
		args = append(args, string(q.Language))
		sql += fmt.Sprintf(" AND language = $%d", len(args))
	}
	if q.Intent != "" {
		args = append(args, string(q.Intent))
		sql += fmt.Sprintf(" AND intent_category = $%d", len(args))
	}
	sql += " ORDER BY entry_id"
	return sql, args
}

func scanFAQRow(rows pgx.Rows) (customerservice.FAQEntry, error) {
	var (
		entry    customerservice.FAQEntry
		language string
		intent   string
	)
	if err := rows.Scan(
		&entry.TenantID,
		&entry.EntryID,
		&language,
		&intent,
		&entry.Question,
		&entry.Answer,
		&entry.Keywords,
	); err != nil {
		return customerservice.FAQEntry{}, fmt.Errorf("faq_entries scan: %w", err)
	}
	entry.Language = customerservice.Language(language)
	entry.IntentCategory = customerservice.Intent(intent)
	return entry, nil
}

// Static interface adherence assertion. Compilation fails fast if
// either side drifts.
var _ customerservice.FAQStore = (*FAQStore)(nil)

package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nfsarch33/helixon-ec/internal/tenant"
)

func TestTenantSettingsRepositoryGetSettingsDecodesJSONColumns(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 1, 2, 3, 0, time.UTC)
	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*[]byte) = []byte(`{"store_name":"Tenant A","primary_color":"#111111"}`)
			*dest[1].(*[]byte) = []byte(`{"store_url":"https://a.example","consumer_key_ref":"secret/a/key"}`)
			*dest[2].(*[]byte) = []byte(`{"content_tone":"friendly","model_tier":"fast","auto_generate_seo":true,"fact_check_required":true}`)
			*dest[3].(*[]byte) = []byte(`{"disabled_rule_ids":["seo_minimum_score"],"severity_override":{"image_alt_text":"warning"},"seo_score_min":82}`)
			*dest[4].(*time.Time) = now
			return nil
		}},
	}
	repo := &TenantSettingsRepository{pool: store}

	got, err := repo.GetSettings(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got.TenantID != "tenant-a" || got.Branding.StoreName != "Tenant A" || got.Compliance.SEOScoreMin != 82 {
		t.Fatalf("settings = %+v", got)
	}
	if got.UpdatedAt != now {
		t.Fatalf("updated_at = %s, want %s", got.UpdatedAt, now)
	}
}

func TestTenantSettingsRepositoryGetSettingsNotFound(t *testing.T) {
	t.Parallel()

	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(_ ...any) error {
			return pgx.ErrNoRows
		}},
	}
	repo := &TenantSettingsRepository{pool: store}

	if _, err := repo.GetSettings(context.Background(), "tenant-a"); err != tenant.ErrSettingsNotFound {
		t.Fatalf("GetSettings error = %v, want ErrSettingsNotFound", err)
	}
}

func TestTenantSettingsRepositoryPutSettingsMarshalsJSONColumns(t *testing.T) {
	t.Parallel()

	store := &mockProductStore{}
	repo := &TenantSettingsRepository{pool: store}
	settings := tenant.Settings{
		TenantID:    "tenant-a",
		Branding:    tenant.BrandingSettings{StoreName: "Tenant A"},
		WooCommerce: tenant.WooCredentialRefs{StoreURL: "https://a.example"},
		AI:          tenant.AIPreferences{ContentTone: "friendly", ModelTier: "fast", AutoGenerateSEO: true},
		Compliance:  tenant.ComplianceRuleOverrides{SEOScoreMin: 82},
		UpdatedAt:   time.Date(2026, 5, 8, 1, 2, 3, 0, time.UTC),
	}

	if err := repo.PutSettings(context.Background(), settings); err != nil {
		t.Fatalf("PutSettings: %v", err)
	}
	if len(store.execCalls) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(store.execCalls))
	}
	call := store.execCalls[0]
	if call.Args[0] != "tenant-a" {
		t.Fatalf("tenant arg = %v, want tenant-a", call.Args[0])
	}
	var branding map[string]any
	if err := json.Unmarshal(call.Args[1].([]byte), &branding); err != nil {
		t.Fatalf("branding json: %v", err)
	}
	if branding["store_name"] != "Tenant A" {
		t.Fatalf("branding json = %+v", branding)
	}
}

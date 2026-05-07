package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/tenant"
)

type TenantSettingsRepository struct {
	pool productStore
}

func NewTenantSettingsRepository(pool *pgxpool.Pool) *TenantSettingsRepository {
	return &TenantSettingsRepository{pool: pool}
}

func (r *TenantSettingsRepository) GetSettings(ctx context.Context, tenantID tenant.ID) (tenant.Settings, error) {
	tenantID, err := tenant.RequireID(tenantID)
	if err != nil {
		return tenant.Settings{}, err
	}
	const q = `
		SELECT branding, woocommerce, ai_preferences, compliance_overrides, updated_at
		FROM tenant_settings WHERE tenant_id = $1`
	var settings tenant.Settings
	var brandingRaw, wooRaw, aiRaw, complianceRaw []byte
	settings.TenantID = tenantID
	err = r.pool.QueryRow(ctx, q, string(tenantID)).Scan(&brandingRaw, &wooRaw, &aiRaw, &complianceRaw, &settings.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tenant.Settings{}, tenant.ErrSettingsNotFound
		}
		return tenant.Settings{}, fmt.Errorf("get tenant settings %s: %w", tenantID, err)
	}
	if err := decodeTenantJSON(brandingRaw, &settings.Branding); err != nil {
		return tenant.Settings{}, err
	}
	if err := decodeTenantJSON(wooRaw, &settings.WooCommerce); err != nil {
		return tenant.Settings{}, err
	}
	if err := decodeTenantJSON(aiRaw, &settings.AI); err != nil {
		return tenant.Settings{}, err
	}
	if err := decodeTenantJSON(complianceRaw, &settings.Compliance); err != nil {
		return tenant.Settings{}, err
	}
	return settings, nil
}

func (r *TenantSettingsRepository) PutSettings(ctx context.Context, settings tenant.Settings) error {
	tenantID, err := tenant.RequireID(settings.TenantID)
	if err != nil {
		return err
	}
	brandingRaw, err := json.Marshal(settings.Branding)
	if err != nil {
		return err
	}
	wooRaw, err := json.Marshal(settings.WooCommerce)
	if err != nil {
		return err
	}
	aiRaw, err := json.Marshal(settings.AI)
	if err != nil {
		return err
	}
	complianceRaw, err := json.Marshal(settings.Compliance)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO tenant_settings (tenant_id, branding, woocommerce, ai_preferences, compliance_overrides, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id) DO UPDATE
		SET branding = EXCLUDED.branding,
		    woocommerce = EXCLUDED.woocommerce,
		    ai_preferences = EXCLUDED.ai_preferences,
		    compliance_overrides = EXCLUDED.compliance_overrides,
		    updated_at = EXCLUDED.updated_at`
	if _, err := r.pool.Exec(ctx, q, string(tenantID), brandingRaw, wooRaw, aiRaw, complianceRaw, settings.UpdatedAt); err != nil {
		return fmt.Errorf("upsert tenant settings %s: %w", tenantID, err)
	}
	return nil
}

func decodeTenantJSON(raw []byte, out any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

package tenant

import (
	"context"
	"testing"
)

func TestServiceStoresTenantSettingsInIsolation(t *testing.T) {
	t.Parallel()

	service := NewService(NewInMemoryRepository())
	ctx := context.Background()

	tenantA := Settings{
		TenantID: "tenant-a",
		Branding: BrandingSettings{
			StoreName:    "Tenant A Store",
			PrimaryColor: "#123456",
		},
		WooCommerce: WooCredentialRefs{
			StoreURL:          "https://tenant-a.example",
			ConsumerKeyRef:    "secret/tenant-a/key",
			ConsumerSecretRef: "secret/tenant-a/secret",
		},
		AI: AIPreferences{
			ContentTone:       "friendly",
			ModelTier:         "fast",
			AutoGenerateSEO:   true,
			FactCheckRequired: true,
		},
		Compliance: ComplianceRuleOverrides{
			DisabledRuleIDs:  []string{"seo_minimum_score"},
			SeverityOverride: map[string]string{"image_alt_text": "warning"},
			SEOScoreMin:      82,
		},
	}
	tenantB := tenantA
	tenantB.TenantID = "tenant-b"
	tenantB.Branding.StoreName = "Tenant B Store"
	tenantB.WooCommerce.StoreURL = "https://tenant-b.example"

	if err := service.PutSettings(ctx, tenantA); err != nil {
		t.Fatalf("put tenant A settings: %v", err)
	}
	if err := service.PutSettings(ctx, tenantB); err != nil {
		t.Fatalf("put tenant B settings: %v", err)
	}

	gotA, err := service.GetSettings(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("get tenant A settings: %v", err)
	}
	gotB, err := service.GetSettings(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("get tenant B settings: %v", err)
	}

	if gotA.Branding.StoreName != "Tenant A Store" || gotB.Branding.StoreName != "Tenant B Store" {
		t.Fatalf("settings leaked across tenants: A=%+v B=%+v", gotA.Branding, gotB.Branding)
	}
	if gotA.Compliance.SEOScoreMin != 82 {
		t.Fatalf("SEOScoreMin = %d, want 82", gotA.Compliance.SEOScoreMin)
	}
}

func TestServiceRejectsMissingTenantSettings(t *testing.T) {
	t.Parallel()

	service := NewService(NewInMemoryRepository())

	if err := service.PutSettings(context.Background(), Settings{}); err == nil {
		t.Fatal("PutSettings accepted missing tenant")
	}
	if _, err := service.GetSettings(context.Background(), " "); err == nil {
		t.Fatal("GetSettings accepted missing tenant")
	}
}

func TestServiceReturnsDefaultSettingsWhenMissing(t *testing.T) {
	t.Parallel()

	service := NewService(NewInMemoryRepository())
	got, err := service.GetSettings(context.Background(), "tenant-defaults")
	if err != nil {
		t.Fatalf("GetSettings missing tenant: %v", err)
	}
	if got.TenantID != "tenant-defaults" || got.Branding.StoreName != "tenant-defaults" {
		t.Fatalf("default settings = %+v", got)
	}
	if got.Compliance.SEOScoreMin != 70 || !got.AI.AutoGenerateSEO || !got.AI.FactCheckRequired {
		t.Fatalf("default preferences = %+v", got)
	}
}

func TestRequiredFromContextRejectsMissingTenant(t *testing.T) {
	t.Parallel()

	if _, err := RequiredFromContext(context.Background()); err == nil {
		t.Fatal("RequiredFromContext accepted empty context")
	}
	got, err := RequiredFromContext(WithID(context.Background(), "tenant-context"))
	if err != nil {
		t.Fatalf("RequiredFromContext: %v", err)
	}
	if got != "tenant-context" {
		t.Fatalf("tenant id = %q, want tenant-context", got)
	}
}

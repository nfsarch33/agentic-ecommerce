package tenant

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

type ID string

const Default ID = "default"

var (
	ErrTenantRequired   = errors.New("tenant id required")
	ErrSettingsNotFound = errors.New("tenant settings not found")
)

type ctxKey struct{}

func WithID(ctx context.Context, id ID) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func FromContext(ctx context.Context) ID {
	id, _ := ctx.Value(ctxKey{}).(ID)
	if id == "" {
		return Default
	}
	return id
}

func RequiredFromContext(ctx context.Context) (ID, error) {
	id, _ := ctx.Value(ctxKey{}).(ID)
	return RequireID(id)
}

func RequireID(id ID) (ID, error) {
	trimmed := ID(strings.TrimSpace(string(id)))
	if trimmed == "" {
		return "", ErrTenantRequired
	}
	return trimmed, nil
}

type Settings struct {
	TenantID    ID                      `json:"tenant_id"`
	Branding    BrandingSettings        `json:"branding"`
	WooCommerce WooCredentialRefs       `json:"woocommerce"`
	AI          AIPreferences           `json:"ai"`
	Compliance  ComplianceRuleOverrides `json:"compliance"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

type BrandingSettings struct {
	StoreName    string `json:"store_name,omitempty"`
	LogoURL      string `json:"logo_url,omitempty"`
	PrimaryColor string `json:"primary_color,omitempty"`
	AccentColor  string `json:"accent_color,omitempty"`
}

type WooCredentialRefs struct {
	StoreURL          string `json:"store_url,omitempty"`
	ConsumerKeyRef    string `json:"consumer_key_ref,omitempty"`
	ConsumerSecretRef string `json:"consumer_secret_ref,omitempty"`
}

type AIPreferences struct {
	ContentTone       string `json:"content_tone,omitempty"`
	ModelTier         string `json:"model_tier,omitempty"`
	AutoGenerateSEO   bool   `json:"auto_generate_seo"`
	FactCheckRequired bool   `json:"fact_check_required"`
}

type ComplianceRuleOverrides struct {
	DisabledRuleIDs  []string          `json:"disabled_rule_ids,omitempty"`
	SeverityOverride map[string]string `json:"severity_override,omitempty"`
	SEOScoreMin      int               `json:"seo_score_min,omitempty"`
}

type Repository interface {
	GetSettings(ctx context.Context, tenantID ID) (Settings, error)
	PutSettings(ctx context.Context, settings Settings) error
}

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

func (s *Service) GetSettings(ctx context.Context, tenantID ID) (Settings, error) {
	tenantID, err := RequireID(tenantID)
	if err != nil {
		return Settings{}, err
	}
	if s == nil || s.repo == nil {
		return DefaultSettings(tenantID), nil
	}
	settings, err := s.repo.GetSettings(ctx, tenantID)
	if errors.Is(err, ErrSettingsNotFound) {
		return DefaultSettings(tenantID), nil
	}
	if err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (s *Service) PutSettings(ctx context.Context, settings Settings) error {
	tenantID, err := RequireID(settings.TenantID)
	if err != nil {
		return err
	}
	settings.TenantID = tenantID
	if settings.UpdatedAt.IsZero() {
		now := time.Now
		if s != nil && s.now != nil {
			now = s.now
		}
		settings.UpdatedAt = now().UTC()
	}
	if s == nil || s.repo == nil {
		return errors.New("tenant settings repository required")
	}
	return s.repo.PutSettings(ctx, settings)
}

func DefaultSettings(tenantID ID) Settings {
	return Settings{
		TenantID: tenantID,
		Branding: BrandingSettings{
			StoreName: string(tenantID),
		},
		AI: AIPreferences{
			ContentTone:       "friendly",
			ModelTier:         "fast",
			AutoGenerateSEO:   true,
			FactCheckRequired: true,
		},
		Compliance: ComplianceRuleOverrides{
			SEOScoreMin: 70,
		},
	}
}

type InMemoryRepository struct {
	mu       sync.RWMutex
	settings map[ID]Settings
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{settings: map[ID]Settings{}}
}

func (r *InMemoryRepository) GetSettings(_ context.Context, tenantID ID) (Settings, error) {
	tenantID, err := RequireID(tenantID)
	if err != nil {
		return Settings{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	settings, ok := r.settings[tenantID]
	if !ok {
		return Settings{}, ErrSettingsNotFound
	}
	return cloneSettings(settings), nil
}

func (r *InMemoryRepository) PutSettings(_ context.Context, settings Settings) error {
	tenantID, err := RequireID(settings.TenantID)
	if err != nil {
		return err
	}
	settings.TenantID = tenantID
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settings[tenantID] = cloneSettings(settings)
	return nil
}

func cloneSettings(settings Settings) Settings {
	if settings.Compliance.DisabledRuleIDs != nil {
		settings.Compliance.DisabledRuleIDs = append([]string(nil), settings.Compliance.DisabledRuleIDs...)
	}
	if settings.Compliance.SeverityOverride != nil {
		overrides := make(map[string]string, len(settings.Compliance.SeverityOverride))
		for k, v := range settings.Compliance.SeverityOverride {
			overrides[k] = v
		}
		settings.Compliance.SeverityOverride = overrides
	}
	return settings
}

package stubsecrets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/stubsecrets"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

func TestStore_GetTenantSecret(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		seed     map[string]string
		tenantID string
		key      string
		want     string
		wantErr  error
	}{
		{
			name:     "hit returns seeded value",
			seed:     map[string]string{"acme/stripe-api-key": "sk_test_acme"},
			tenantID: "acme",
			key:      "stripe-api-key",
			want:     "sk_test_acme",
		},
		{
			name:     "miss returns ErrSecretNotFound",
			seed:     map[string]string{"acme/stripe-api-key": "sk_test_acme"},
			tenantID: "umbrella",
			key:      "stripe-api-key",
			wantErr:  port.ErrSecretNotFound,
		},
		{
			name:     "empty tenant rejected",
			seed:     nil,
			tenantID: "",
			key:      "stripe-api-key",
			wantErr:  port.ErrSecretInvalidArgument,
		},
		{
			name:     "empty key rejected",
			seed:     nil,
			tenantID: "acme",
			key:      "",
			wantErr:  port.ErrSecretInvalidArgument,
		},
		{
			name:     "uppercase rejected",
			seed:     nil,
			tenantID: "ACME",
			key:      "stripe-api-key",
			wantErr:  port.ErrSecretInvalidArgument,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := stubsecrets.NewStore(tc.seed)
			got, err := s.GetTenantSecret(context.Background(), tc.tenantID, tc.key)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v; want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestStore_Set_RoundTrip(t *testing.T) {
	t.Parallel()
	s := stubsecrets.NewStore(nil)
	if err := s.Set("tenant-a", "license-hmac", "k1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set("tenant-a", "license-hmac", "k2"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	got, err := s.GetTenantSecret(context.Background(), "tenant-a", "license-hmac")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "k2" {
		t.Fatalf("got=%q want=k2", got)
	}
}

func TestStore_Set_RejectsInvalid(t *testing.T) {
	t.Parallel()
	s := stubsecrets.NewStore(nil)
	if err := s.Set("", "k", "v"); !errors.Is(err, port.ErrSecretInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if err := s.Set("acme", "Bad Key", "v"); !errors.Is(err, port.ErrSecretInvalidArgument) {
		t.Fatalf("expected invalid argument for spaced key, got %v", err)
	}
}

func TestStore_ConstantsExposed(t *testing.T) {
	t.Parallel()
	wantKeys := []string{
		port.SecretKeyStripeAPIKey,
		port.SecretKeyStripeWebhookSecret,
		port.SecretKeyLicenseHMACKey,
		port.SecretKeyURLHMACKey,
		port.SecretKeyRegistrationHMACKey,
		port.SecretKeyMarketplaceWebhook,
		port.SecretKeyTenantOverrideAPIKey,
	}
	for _, k := range wantKeys {
		if k == "" {
			t.Fatalf("secret key constant must not be empty")
		}
	}
}

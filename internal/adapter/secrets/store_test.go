package secrets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/secrets"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

type fakeResolver struct {
	values  map[string]string
	calls   []string
	failErr error
}

func (f *fakeResolver) Resolve(_ context.Context, path string) (string, error) {
	f.calls = append(f.calls, path)
	if f.failErr != nil {
		return "", f.failErr
	}
	v, ok := f.values[path]
	if !ok {
		return "", port.ErrSecretNotFound
	}
	return v, nil
}

func TestNew_Validation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		backend secrets.Backend
		opts    []secrets.Option
		wantErr string
	}{
		{
			name:    "stub backend valid without options",
			backend: secrets.BackendStub,
		},
		{
			name:    "aws backend requires resolver",
			backend: secrets.BackendAWS,
			wantErr: "backend=aws requires WithCloudResolver",
		},
		{
			name:    "aws backend valid with resolver",
			backend: secrets.BackendAWS,
			opts:    []secrets.Option{secrets.WithCloudResolver(&fakeResolver{})},
		},
		{
			name:    "gcp backend requires resolver",
			backend: secrets.BackendGCP,
			opts:    []secrets.Option{secrets.WithGCPProject("p")},
			wantErr: "backend=gcp requires WithCloudResolver",
		},
		{
			name:    "gcp backend requires project",
			backend: secrets.BackendGCP,
			opts:    []secrets.Option{secrets.WithCloudResolver(&fakeResolver{})},
			wantErr: "backend=gcp requires WithGCPProject",
		},
		{
			name:    "gcp backend valid with both",
			backend: secrets.BackendGCP,
			opts: []secrets.Option{
				secrets.WithCloudResolver(&fakeResolver{}),
				secrets.WithGCPProject("p"),
			},
		},
		{
			name:    "unknown backend rejected",
			backend: secrets.Backend(99),
			wantErr: "unknown backend",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, err := secrets.New(tc.backend, tc.opts...)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%q want substring=%q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if m == nil {
				t.Fatalf("expected manager, got nil")
			}
			if m.Backend() != tc.backend {
				t.Fatalf("Backend()=%v want=%v", m.Backend(), tc.backend)
			}
		})
	}
}

func TestStub_GetTenantSecret(t *testing.T) {
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
			tenantID: "",
			key:      "stripe-api-key",
			wantErr:  port.ErrSecretInvalidArgument,
		},
		{
			name:     "empty key rejected",
			tenantID: "acme",
			key:      "",
			wantErr:  port.ErrSecretInvalidArgument,
		},
		{
			name:     "uppercase rejected",
			tenantID: "ACME",
			key:      "stripe-api-key",
			wantErr:  port.ErrSecretInvalidArgument,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, err := secrets.New(secrets.BackendStub, secrets.WithSeed(tc.seed))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			got, err := m.GetTenantSecret(context.Background(), tc.tenantID, tc.key)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err=%v want=%v", err, tc.wantErr)
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

func TestStub_Set_RoundTrip(t *testing.T) {
	t.Parallel()
	m, err := secrets.New(secrets.BackendStub)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Set("tenant-a", "license-hmac", "k1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := m.Set("tenant-a", "license-hmac", "k2"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	got, err := m.GetTenantSecret(context.Background(), "tenant-a", "license-hmac")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "k2" {
		t.Fatalf("got=%q want=k2", got)
	}
}

func TestStub_Set_RejectsInvalid(t *testing.T) {
	t.Parallel()
	m, _ := secrets.New(secrets.BackendStub)
	if err := m.Set("", "k", "v"); !errors.Is(err, port.ErrSecretInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if err := m.Set("acme", "Bad Key", "v"); !errors.Is(err, port.ErrSecretInvalidArgument) {
		t.Fatalf("expected invalid argument for spaced key, got %v", err)
	}
}

func TestSet_OnNonStubRejected(t *testing.T) {
	t.Parallel()
	m, err := secrets.New(secrets.BackendAWS, secrets.WithCloudResolver(&fakeResolver{}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = m.Set("acme", "license-hmac", "v")
	if err == nil || !contains(err.Error(), "Set only valid for backend=stub") {
		t.Fatalf("expected backend=stub guard, got %v", err)
	}
}

func TestAWS_GetTenantSecret(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		prefix   string
		tenant   string
		key      string
		wantPath string
	}{
		{
			name:     "default prefix",
			prefix:   "",
			tenant:   "acme",
			key:      port.SecretKeyStripeAPIKey,
			wantPath: "agentic-ecommerce/acme/stripe-api-key",
		},
		{
			name:     "custom prefix",
			prefix:   "ec-prod",
			tenant:   "umbrella",
			key:      port.SecretKeyLicenseHMACKey,
			wantPath: "ec-prod/umbrella/license-hmac",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fr := &fakeResolver{values: map[string]string{tc.wantPath: "secret-value"}}
			m, err := secrets.New(secrets.BackendAWS,
				secrets.WithCloudResolver(fr),
				secrets.WithPrefix(tc.prefix),
			)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			got, err := m.GetTenantSecret(context.Background(), tc.tenant, tc.key)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != "secret-value" {
				t.Fatalf("got=%q want=secret-value", got)
			}
			if len(fr.calls) != 1 || fr.calls[0] != tc.wantPath {
				t.Fatalf("called=%v want=[%s]", fr.calls, tc.wantPath)
			}

			path, err := m.Path(tc.tenant, tc.key)
			if err != nil {
				t.Fatalf("Path: %v", err)
			}
			if path != tc.wantPath {
				t.Fatalf("Path()=%q want=%q", path, tc.wantPath)
			}
		})
	}
}

func TestAWS_RejectsInvalid(t *testing.T) {
	t.Parallel()
	m, _ := secrets.New(secrets.BackendAWS, secrets.WithCloudResolver(&fakeResolver{}))
	cases := []struct {
		name   string
		tenant string
		key    string
	}{
		{"empty tenant", "", "k"},
		{"empty key", "acme", ""},
		{"upper tenant", "ACME", "k"},
		{"space key", "acme", "bad key"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := m.GetTenantSecret(context.Background(), tc.tenant, tc.key)
			if !errors.Is(err, port.ErrSecretInvalidArgument) {
				t.Fatalf("expected invalid argument, got %v", err)
			}
		})
	}
}

func TestAWS_ResolverErrPropagates(t *testing.T) {
	t.Parallel()
	want := errors.New("aws: rate limit")
	m, _ := secrets.New(secrets.BackendAWS, secrets.WithCloudResolver(&fakeResolver{failErr: want}))
	_, err := m.GetTenantSecret(context.Background(), "acme", "k")
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped resolver err, got %v", err)
	}
}

func TestAWS_PathRejectsInvalid(t *testing.T) {
	t.Parallel()
	m, _ := secrets.New(secrets.BackendAWS, secrets.WithCloudResolver(&fakeResolver{}))
	_, err := m.Path("ACME", "k")
	if !errors.Is(err, port.ErrSecretInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestAWS_PathOnNonAWSRejected(t *testing.T) {
	t.Parallel()
	m, _ := secrets.New(secrets.BackendStub)
	_, err := m.Path("acme", "k")
	if err == nil || !contains(err.Error(), "Path only valid for backend=aws") {
		t.Fatalf("expected backend=aws guard, got %v", err)
	}
}

func TestGCP_GetTenantSecret(t *testing.T) {
	t.Parallel()

	want := "projects/proj-1/secrets/agentic-ecommerce_acme_stripe-api-key/versions/latest"
	fr := &fakeResolver{values: map[string]string{want: "sk_live_acme"}}
	m, err := secrets.New(secrets.BackendGCP,
		secrets.WithCloudResolver(fr),
		secrets.WithGCPProject("proj-1"),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := m.GetTenantSecret(context.Background(), "acme", port.SecretKeyStripeAPIKey)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "sk_live_acme" {
		t.Fatalf("got=%q want=sk_live_acme", got)
	}

	name, err := m.Name("acme", port.SecretKeyStripeAPIKey)
	if err != nil {
		t.Fatalf("Name: %v", err)
	}
	if name != want {
		t.Fatalf("Name()=%q want=%q", name, want)
	}
	if len(fr.calls) != 1 || fr.calls[0] != want {
		t.Fatalf("called=%v want=[%s]", fr.calls, want)
	}
}

func TestGCP_CustomPrefix(t *testing.T) {
	t.Parallel()
	want := "projects/proj-2/secrets/ec-prod_umbrella_license-hmac/versions/latest"
	fr := &fakeResolver{values: map[string]string{want: "hmac-key"}}
	m, err := secrets.New(secrets.BackendGCP,
		secrets.WithCloudResolver(fr),
		secrets.WithGCPProject("proj-2"),
		secrets.WithPrefix("ec-prod"),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := m.GetTenantSecret(context.Background(), "umbrella", port.SecretKeyLicenseHMACKey)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "hmac-key" {
		t.Fatalf("got=%q want=hmac-key", got)
	}
}

func TestGCP_RejectsInvalid(t *testing.T) {
	t.Parallel()
	m, _ := secrets.New(secrets.BackendGCP,
		secrets.WithCloudResolver(&fakeResolver{}),
		secrets.WithGCPProject("p"),
	)
	cases := []struct {
		name   string
		tenant string
		key    string
	}{
		{"empty tenant", "", "k"},
		{"empty key", "acme", ""},
		{"upper tenant", "ACME", "k"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := m.GetTenantSecret(context.Background(), tc.tenant, tc.key)
			if !errors.Is(err, port.ErrSecretInvalidArgument) {
				t.Fatalf("expected invalid argument, got %v", err)
			}
		})
	}
}

func TestGCP_NameOnNonGCPRejected(t *testing.T) {
	t.Parallel()
	m, _ := secrets.New(secrets.BackendStub)
	_, err := m.Name("acme", "k")
	if err == nil || !contains(err.Error(), "Name only valid for backend=gcp") {
		t.Fatalf("expected backend=gcp guard, got %v", err)
	}
}

func TestBackend_String(t *testing.T) {
	t.Parallel()
	cases := map[secrets.Backend]string{
		secrets.BackendStub: "stub",
		secrets.BackendAWS:  "aws",
		secrets.BackendGCP:  "gcp",
		secrets.Backend(99): "unknown",
	}
	for backend, want := range cases {
		backend, want := backend, want
		t.Run(want, func(t *testing.T) {
			t.Parallel()
			if backend.String() != want {
				t.Fatalf("backend(%d).String()=%q want=%q", backend, backend.String(), want)
			}
		})
	}
}

func TestSecretsConstantsExposed(t *testing.T) {
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

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

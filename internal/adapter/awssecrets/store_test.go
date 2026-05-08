package awssecrets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/awssecrets"
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

func TestStore_GetTenantSecret(t *testing.T) {
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
			s := awssecrets.NewStore(tc.prefix, fr)

			got, err := s.GetTenantSecret(context.Background(), tc.tenant, tc.key)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != "secret-value" {
				t.Fatalf("got=%q want=secret-value", got)
			}
			if len(fr.calls) != 1 || fr.calls[0] != tc.wantPath {
				t.Fatalf("called=%v want=[%s]", fr.calls, tc.wantPath)
			}
			if s.Path(tc.tenant, tc.key) != tc.wantPath {
				t.Fatalf("Path()=%q want=%q", s.Path(tc.tenant, tc.key), tc.wantPath)
			}
		})
	}
}

func TestStore_RejectsInvalid(t *testing.T) {
	t.Parallel()
	s := awssecrets.NewStore("", &fakeResolver{})
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
			_, err := s.GetTenantSecret(context.Background(), tc.tenant, tc.key)
			if !errors.Is(err, port.ErrSecretInvalidArgument) {
				t.Fatalf("expected invalid argument, got %v", err)
			}
		})
	}
}

func TestStore_NilResolver(t *testing.T) {
	t.Parallel()
	s := awssecrets.NewStore("", nil)
	_, err := s.GetTenantSecret(context.Background(), "acme", "k")
	if err == nil || err.Error() == "" {
		t.Fatalf("expected resolver-not-configured err, got %v", err)
	}
}

func TestStore_ResolverErrPropagates(t *testing.T) {
	t.Parallel()
	want := errors.New("aws: rate limit")
	s := awssecrets.NewStore("", &fakeResolver{failErr: want})
	_, err := s.GetTenantSecret(context.Background(), "acme", "k")
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped resolver err, got %v", err)
	}
}

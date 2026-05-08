package gcpsecrets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/gcpsecrets"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

type fakeResolver struct {
	values  map[string]string
	calls   []string
	failErr error
}

func (f *fakeResolver) Resolve(_ context.Context, name string) (string, error) {
	f.calls = append(f.calls, name)
	if f.failErr != nil {
		return "", f.failErr
	}
	v, ok := f.values[name]
	if !ok {
		return "", port.ErrSecretNotFound
	}
	return v, nil
}

func TestStore_GetTenantSecret(t *testing.T) {
	t.Parallel()

	want := "projects/proj-1/secrets/agentic-ecommerce_acme_stripe-api-key/versions/latest"
	fr := &fakeResolver{values: map[string]string{want: "sk_live_acme"}}
	s := gcpsecrets.NewStore("proj-1", "", fr)

	got, err := s.GetTenantSecret(context.Background(), "acme", port.SecretKeyStripeAPIKey)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "sk_live_acme" {
		t.Fatalf("got=%q want=sk_live_acme", got)
	}
	if name := s.Name("acme", port.SecretKeyStripeAPIKey); name != want {
		t.Fatalf("Name()=%q want=%q", name, want)
	}
	if len(fr.calls) != 1 || fr.calls[0] != want {
		t.Fatalf("called=%v want=[%s]", fr.calls, want)
	}
}

func TestStore_CustomPrefix(t *testing.T) {
	t.Parallel()
	want := "projects/proj-2/secrets/ec-prod_umbrella_license-hmac/versions/latest"
	fr := &fakeResolver{values: map[string]string{want: "hmac-key"}}
	s := gcpsecrets.NewStore("proj-2", "ec-prod", fr)
	got, err := s.GetTenantSecret(context.Background(), "umbrella", port.SecretKeyLicenseHMACKey)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "hmac-key" {
		t.Fatalf("got=%q want=hmac-key", got)
	}
}

func TestStore_RejectsInvalid(t *testing.T) {
	t.Parallel()
	s := gcpsecrets.NewStore("p", "", &fakeResolver{})
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
			_, err := s.GetTenantSecret(context.Background(), tc.tenant, tc.key)
			if !errors.Is(err, port.ErrSecretInvalidArgument) {
				t.Fatalf("expected invalid argument, got %v", err)
			}
		})
	}
}

func TestStore_RequiresResolverAndProject(t *testing.T) {
	t.Parallel()
	s := gcpsecrets.NewStore("", "", &fakeResolver{})
	_, err := s.GetTenantSecret(context.Background(), "acme", "k")
	if err == nil {
		t.Fatalf("expected error when project missing")
	}
	s2 := gcpsecrets.NewStore("p", "", nil)
	_, err = s2.GetTenantSecret(context.Background(), "acme", "k")
	if err == nil {
		t.Fatalf("expected error when resolver nil")
	}
}

package digital

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func validAccessGrantInput() AccessGrantInput {
	return AccessGrantInput{
		TenantID:   "tenant-a",
		CustomerID: uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		ProductID:  uuid.MustParse("66666666-6666-6666-6666-666666666666"),
		LicenseID:  uuid.MustParse("77777777-7777-7777-7777-777777777777"),
		Source:     SourcePurchase,
		Now:        time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
	}
}

func TestNewAccessGrantValidates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*AccessGrantInput)
		wantErr error
	}{
		{name: "ok", mutate: func(*AccessGrantInput) {}},
		{name: "missing tenant", mutate: func(in *AccessGrantInput) { in.TenantID = "" }, wantErr: ErrTenantRequired},
		{name: "missing customer", mutate: func(in *AccessGrantInput) { in.CustomerID = uuid.Nil }, wantErr: ErrCustomerRequired},
		{name: "missing product", mutate: func(in *AccessGrantInput) { in.ProductID = uuid.Nil }, wantErr: ErrProductRequired},
		{name: "missing source", mutate: func(in *AccessGrantInput) { in.Source = "" }, wantErr: ErrAccessGrantSourceRequired},
		{name: "invalid source", mutate: func(in *AccessGrantInput) { in.Source = Source("hack") }, wantErr: ErrInvalidSource},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := validAccessGrantInput()
			tc.mutate(&input)
			_, err := NewAccessGrant(input)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestParseSourceRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []Source{SourcePurchase, SourceGift, SourceAdmin} {
		got, err := ParseSource(string(s))
		if err != nil || got != s {
			t.Fatalf("ParseSource(%q) = %q, %v", s, got, err)
		}
	}
	if _, err := ParseSource("rogue"); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("ParseSource rogue err = %v", err)
	}
}

package digital

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func validLicenseInput() LicenseInput {
	return LicenseInput{
		TenantID:       "tenant-a",
		ProductID:      uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		CustomerID:     uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Key:            "AAAAA-BBBBB-CCCCC-DDDDD-EEEEEEEE",
		MaxActivations: 3,
		Now:            time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
	}
}

func TestNewLicenseValidates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*LicenseInput)
		wantErr error
	}{
		{name: "ok", mutate: func(*LicenseInput) {}},
		{name: "missing tenant", mutate: func(in *LicenseInput) { in.TenantID = "  " }, wantErr: ErrTenantRequired},
		{name: "missing product", mutate: func(in *LicenseInput) { in.ProductID = uuid.Nil }, wantErr: ErrProductRequired},
		{name: "missing customer", mutate: func(in *LicenseInput) { in.CustomerID = uuid.Nil }, wantErr: ErrCustomerRequired},
		{name: "missing key", mutate: func(in *LicenseInput) { in.Key = "" }, wantErr: ErrLicenseKeyRequired},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := validLicenseInput()
			tc.mutate(&input)
			_, err := NewLicense(input)
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

func TestLicenseStateMachineApplyLegalAndIllegal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		from    State
		via     Transition
		to      State
		wantErr bool
	}{
		{name: "active->revoke", from: StateActive, via: TransitionRevoke, to: StateRevoked},
		{name: "active->expire", from: StateActive, via: TransitionExpire, to: StateExpired},
		{name: "revoked->revoke (illegal)", from: StateRevoked, via: TransitionRevoke, wantErr: true},
		{name: "revoked->expire (illegal)", from: StateRevoked, via: TransitionExpire, wantErr: true},
		{name: "expired->revoke (illegal)", from: StateExpired, via: TransitionRevoke, wantErr: true},
		{name: "expired->expire (illegal)", from: StateExpired, via: TransitionExpire, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lic, err := NewLicense(validLicenseInput())
			if err != nil {
				t.Fatalf("NewLicense: %v", err)
			}
			// Force the from state via reflection-free mutation through
			// ReconstructLicense.
			rec := LicenseRecord{
				ID:         lic.ID(),
				TenantID:   lic.TenantID(),
				ProductID:  lic.ProductID(),
				CustomerID: lic.CustomerID(),
				Key:        lic.Key(),
				State:      tc.from,
				IssuedAt:   lic.IssuedAt(),
				UpdatedAt:  lic.UpdatedAt(),
			}
			lic = ReconstructLicense(rec)
			now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
			err = lic.Apply(tc.via, now)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("Apply(%q->%q) err = %v, want ErrInvalidTransition", tc.from, tc.via, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if lic.State() != tc.to {
				t.Fatalf("state = %q, want %q", lic.State(), tc.to)
			}
		})
	}
}

func TestLicenseCheckActiveReportsExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	expires := now.Add(5 * time.Minute)
	input := validLicenseInput()
	input.ExpiresAt = expires
	input.Now = now
	lic, err := NewLicense(input)
	if err != nil {
		t.Fatalf("NewLicense: %v", err)
	}
	if err := lic.CheckActive(now); err != nil {
		t.Fatalf("CheckActive at issue time: %v", err)
	}
	if err := lic.CheckActive(expires.Add(time.Second)); !errors.Is(err, ErrLicenseExpired) {
		t.Fatalf("CheckActive after expiry: %v", err)
	}
}

func TestLicenseCheckActiveReportsRevoked(t *testing.T) {
	t.Parallel()
	lic, err := NewLicense(validLicenseInput())
	if err != nil {
		t.Fatalf("NewLicense: %v", err)
	}
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	if err := lic.Apply(TransitionRevoke, now); err != nil {
		t.Fatalf("Apply revoke: %v", err)
	}
	if err := lic.CheckActive(now); !errors.Is(err, ErrLicenseRevoked) {
		t.Fatalf("CheckActive revoked: %v", err)
	}
}

func TestLicenseAcceptsZeroExpiry(t *testing.T) {
	t.Parallel()
	input := validLicenseInput()
	input.ExpiresAt = time.Time{}
	lic, err := NewLicense(input)
	if err != nil {
		t.Fatalf("NewLicense (no expiry): %v", err)
	}
	if !lic.ExpiresAt().IsZero() {
		t.Fatalf("ExpiresAt should be zero, got %s", lic.ExpiresAt())
	}
}

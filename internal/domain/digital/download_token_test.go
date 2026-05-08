package digital

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func validDownloadTokenInput() DownloadTokenInput {
	return DownloadTokenInput{
		TenantID:    "tenant-a",
		LicenseID:   uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		Signature:   "deadbeefcafef00d",
		ExpiresAt:   time.Date(2026, 5, 8, 12, 5, 0, 0, time.UTC),
		UsesAllowed: 3,
		Now:         time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
	}
}

func TestNewDownloadTokenValidates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*DownloadTokenInput)
		wantErr error
	}{
		{name: "ok", mutate: func(*DownloadTokenInput) {}},
		{name: "missing tenant", mutate: func(in *DownloadTokenInput) { in.TenantID = "" }, wantErr: ErrTenantRequired},
		{name: "missing signature", mutate: func(in *DownloadTokenInput) { in.Signature = "" }, wantErr: ErrDownloadSignatureRequired},
		{name: "expiry in past", mutate: func(in *DownloadTokenInput) { in.ExpiresAt = in.Now.Add(-time.Minute) }, wantErr: ErrDownloadInvalidExpiry},
		{name: "expiry equals now", mutate: func(in *DownloadTokenInput) { in.ExpiresAt = in.Now }, wantErr: ErrDownloadInvalidExpiry},
		{name: "uses allowed zero", mutate: func(in *DownloadTokenInput) { in.UsesAllowed = 0 }, wantErr: ErrDownloadInvalidUsesAllowed},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := validDownloadTokenInput()
			tc.mutate(&input)
			_, err := NewDownloadToken(input)
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

func TestDownloadTokenCheckUsableExpiry(t *testing.T) {
	t.Parallel()
	input := validDownloadTokenInput()
	tok, err := NewDownloadToken(input)
	if err != nil {
		t.Fatalf("NewDownloadToken: %v", err)
	}
	if err := tok.CheckUsable(input.Now); err != nil {
		t.Fatalf("CheckUsable at issue time: %v", err)
	}
	if err := tok.CheckUsable(input.ExpiresAt); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("CheckUsable at exact expiry: %v, want ErrTokenExpired", err)
	}
	if err := tok.CheckUsable(input.ExpiresAt.Add(time.Second)); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("CheckUsable past expiry: %v, want ErrTokenExpired", err)
	}
}

func TestDownloadTokenMarkUsedExhaustsCap(t *testing.T) {
	t.Parallel()
	input := validDownloadTokenInput()
	input.UsesAllowed = 2
	tok, err := NewDownloadToken(input)
	if err != nil {
		t.Fatalf("NewDownloadToken: %v", err)
	}
	if err := tok.MarkUsed(input.Now); err != nil {
		t.Fatalf("MarkUsed 1: %v", err)
	}
	if err := tok.MarkUsed(input.Now); err != nil {
		t.Fatalf("MarkUsed 2: %v", err)
	}
	if err := tok.MarkUsed(input.Now); !errors.Is(err, ErrMaxUsesExceeded) {
		t.Fatalf("MarkUsed 3 over cap: %v, want ErrMaxUsesExceeded", err)
	}
}

func TestDownloadTokenMarkUsedRejectsExpired(t *testing.T) {
	t.Parallel()
	input := validDownloadTokenInput()
	tok, err := NewDownloadToken(input)
	if err != nil {
		t.Fatalf("NewDownloadToken: %v", err)
	}
	if err := tok.MarkUsed(input.ExpiresAt); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("MarkUsed at expiry: %v, want ErrTokenExpired", err)
	}
}

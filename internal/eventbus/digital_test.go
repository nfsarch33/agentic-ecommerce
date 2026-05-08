package eventbus

import (
	"errors"
	"testing"
	"time"
)

func TestIsDigitalEvent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   EventType
		want bool
	}{
		{name: "product created", in: DigitalProductCreated, want: true},
		{name: "product updated", in: DigitalProductUpdated, want: true},
		{name: "product deleted", in: DigitalProductDeleted, want: true},
		{name: "purchased", in: DigitalPurchased, want: true},
		{name: "downloaded", in: DigitalDownloaded, want: true},
		{name: "license activated", in: LicenseActivated, want: true},
		{name: "license revoked", in: LicenseRevoked, want: true},
		{name: "license expired", in: LicenseExpired, want: true},
		{name: "membership not digital", in: MembershipCreated, want: false},
		{name: "empty not digital", in: "", want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsDigitalEvent(tc.in); got != tc.want {
				t.Fatalf("IsDigitalEvent(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestDigitalPayloadValidate(t *testing.T) {
	t.Parallel()
	base := DigitalPayload{
		Version:   DigitalPayloadVersion,
		TenantID:  "tenant-a",
		ProductID: "prod-1",
		LicenseID: "lic-1",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*DigitalPayload)
	}{
		{"missing version", func(p *DigitalPayload) { p.Version = 0 }},
		{"missing tenant", func(p *DigitalPayload) { p.TenantID = "" }},
		{"missing product and license", func(p *DigitalPayload) {
			p.ProductID = ""
			p.LicenseID = ""
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := base
			tc.mutate(&p)
			if err := p.Validate(); !errors.Is(err, ErrDigitalPayloadInvalid) {
				t.Fatalf("Validate err = %v, want ErrDigitalPayloadInvalid", err)
			}
		})
	}
}

func TestNewDigitalEventDefaultsAndValidates(t *testing.T) {
	t.Parallel()
	p := DigitalPayload{TenantID: "tenant-a", LicenseID: "lic-1"}
	evt, err := NewDigitalEvent(LicenseActivated, "", time.Time{}, p)
	if err != nil {
		t.Fatalf("NewDigitalEvent: %v", err)
	}
	if evt.TenantID != "tenant-a" {
		t.Fatalf("TenantID = %q", evt.TenantID)
	}
	if evt.Source != "mc-api.digital" {
		t.Fatalf("default source = %q", evt.Source)
	}
	if evt.Timestamp.IsZero() {
		t.Fatal("Timestamp should be auto-stamped")
	}
	if evt.Payload["version"].(int) != DigitalPayloadVersion {
		t.Fatalf("version not defaulted")
	}
}

func TestNewDigitalEventRejectsNonDigitalType(t *testing.T) {
	t.Parallel()
	p := DigitalPayload{Version: 1, TenantID: "tenant-a", LicenseID: "lic-1"}
	if _, err := NewDigitalEvent(MembershipCreated, "", time.Time{}, p); !errors.Is(err, ErrDigitalPayloadInvalid) {
		t.Fatalf("err = %v, want ErrDigitalPayloadInvalid", err)
	}
}

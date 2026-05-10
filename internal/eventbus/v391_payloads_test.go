// File scope: v3.9.1 typed event payload RED tests.
package eventbus

import (
	"errors"
	"testing"
	"time"
)

func TestTenantOnboardedPayload_Validate(t *testing.T) {
	t.Parallel()
	good := TenantOnboardedPayload{
		Version:      TenantOnboardedPayloadVersion,
		TenantID:     "tenant-1",
		WizardID:     "wiz-1",
		BusinessType: "company",
		Country:      "AU",
		Channels:     []string{"tiktok"},
		Compliance:   []string{"au_consumer_law"},
		OccurredAt:   time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("good payload failed: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(p *TenantOnboardedPayload)
		wantErr error
	}{
		{"version zero", func(p *TenantOnboardedPayload) { p.Version = 0 }, ErrTenantOnboardedInvalid},
		{"missing tenant", func(p *TenantOnboardedPayload) { p.TenantID = "" }, ErrTenantOnboardedInvalid},
		{"missing wizard", func(p *TenantOnboardedPayload) { p.WizardID = "" }, ErrTenantOnboardedInvalid},
		{"missing business type", func(p *TenantOnboardedPayload) { p.BusinessType = "" }, ErrTenantOnboardedInvalid},
		{"missing country", func(p *TenantOnboardedPayload) { p.Country = "" }, ErrTenantOnboardedInvalid},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := good
			c.mutate(&p)
			err := p.Validate()
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate err=%v want=%v", err, c.wantErr)
			}
		})
	}
}

func TestNewTenantOnboardedEvent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	evt, err := NewTenantOnboardedEvent("", time.Time{}, TenantOnboardedPayload{
		TenantID:     "tenant-1",
		WizardID:     "wiz-1",
		BusinessType: "company",
		Country:      "AU",
		OccurredAt:   now,
	})
	if err != nil {
		t.Fatalf("NewTenantOnboardedEvent: %v", err)
	}
	if evt.Type != TenantOnboarded {
		t.Fatalf("type=%q want=%q", evt.Type, TenantOnboarded)
	}
	if evt.TenantID != "tenant-1" {
		t.Fatalf("tenant=%q want=tenant-1", evt.TenantID)
	}
	if evt.Source == "" {
		t.Fatal("source not defaulted")
	}
	if got, _ := evt.Payload["country"].(string); got != "AU" {
		t.Fatalf("country=%q want=AU", got)
	}

	if _, err := NewTenantOnboardedEvent("test", now, TenantOnboardedPayload{Version: 1, TenantID: ""}); !errors.Is(err, ErrTenantOnboardedInvalid) {
		t.Fatalf("expected ErrTenantOnboardedInvalid for invalid payload; got %v", err)
	}
}

func TestChannelStatusNotYetImplementedPayload_Validate(t *testing.T) {
	t.Parallel()
	good := ChannelStatusNotYetImplementedPayload{
		Version:    ChannelStatusNotYetImplementedPayloadVersion,
		TenantID:   "tenant-1",
		Channel:    "instagram",
		Op:         "publish",
		ProductID:  "sku-1",
		OccurredAt: time.Now().UTC(),
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("good payload failed: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(p *ChannelStatusNotYetImplementedPayload)
	}{
		{"version zero", func(p *ChannelStatusNotYetImplementedPayload) { p.Version = 0 }},
		{"missing tenant", func(p *ChannelStatusNotYetImplementedPayload) { p.TenantID = "" }},
		{"missing channel", func(p *ChannelStatusNotYetImplementedPayload) { p.Channel = "" }},
		{"missing op", func(p *ChannelStatusNotYetImplementedPayload) { p.Op = "" }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := good
			c.mutate(&p)
			if err := p.Validate(); !errors.Is(err, ErrChannelStatusNotYetImplementedInvalid) {
				t.Fatalf("Validate err=%v want=ErrChannelStatusNotYetImplementedInvalid", err)
			}
		})
	}
}

func TestNewChannelStatusNotYetImplementedEvent(t *testing.T) {
	t.Parallel()
	evt, err := NewChannelStatusNotYetImplementedEvent("", time.Time{}, ChannelStatusNotYetImplementedPayload{
		TenantID:  "tenant-1",
		Channel:   "instagram",
		Op:        "publish",
		ProductID: "sku-1",
	})
	if err != nil {
		t.Fatalf("New event: %v", err)
	}
	if evt.Type != ChannelStatusNotYetImplemented {
		t.Fatalf("type mismatch: %q", evt.Type)
	}
	if evt.Source == "" {
		t.Fatal("default source missing")
	}
	if got, _ := evt.Payload["channel"].(string); got != "instagram" {
		t.Fatalf("channel=%q want=instagram", got)
	}
}

func TestOperatorAlertResolvedPayload_Validate(t *testing.T) {
	t.Parallel()
	good := OperatorAlertResolvedPayload{
		Version:    OperatorAlertResolvedPayloadVersion,
		TenantID:   "tenant-1",
		AlertID:    "alert-1",
		AlertType:  "price_change_pending_approval",
		Action:     "approve",
		OccurredAt: time.Now().UTC(),
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("good payload failed: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(p *OperatorAlertResolvedPayload)
	}{
		{"version zero", func(p *OperatorAlertResolvedPayload) { p.Version = 0 }},
		{"missing tenant", func(p *OperatorAlertResolvedPayload) { p.TenantID = "" }},
		{"missing alert", func(p *OperatorAlertResolvedPayload) { p.AlertID = "" }},
		{"missing alert type", func(p *OperatorAlertResolvedPayload) { p.AlertType = "" }},
		{"invalid action", func(p *OperatorAlertResolvedPayload) { p.Action = "ignore" }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := good
			c.mutate(&p)
			if err := p.Validate(); !errors.Is(err, ErrOperatorAlertResolvedInvalid) {
				t.Fatalf("Validate err=%v want=ErrOperatorAlertResolvedInvalid", err)
			}
		})
	}
}

func TestNewOperatorAlertResolvedEvent(t *testing.T) {
	t.Parallel()
	evt, err := NewOperatorAlertResolvedEvent("", time.Time{}, OperatorAlertResolvedPayload{
		TenantID:  "tenant-1",
		AlertID:   "alert-1",
		AlertType: "price_change_pending_approval",
		Action:    "deny",
	})
	if err != nil {
		t.Fatalf("New event: %v", err)
	}
	if evt.Type != OperatorAlertResolved {
		t.Fatalf("type=%q want=%q", evt.Type, OperatorAlertResolved)
	}
	if got, _ := evt.Payload["action"].(string); got != "deny" {
		t.Fatalf("action=%q want=deny", got)
	}
	if _, err := NewOperatorAlertResolvedEvent("", time.Now(), OperatorAlertResolvedPayload{TenantID: "", AlertID: "x", AlertType: "y", Action: "approve"}); !errors.Is(err, ErrOperatorAlertResolvedInvalid) {
		t.Fatalf("expected ErrOperatorAlertResolvedInvalid for invalid payload")
	}
}

package eventbus

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestIsMembershipEvent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   EventType
		want bool
	}{
		{name: "created", in: MembershipCreated, want: true},
		{name: "renewed", in: MembershipRenewed, want: true},
		{name: "cancelled", in: MembershipCancelled, want: true},
		{name: "paused", in: MembershipPaused, want: true},
		{name: "resumed", in: MembershipResumed, want: true},
		{name: "product not membership", in: ProductCreated, want: false},
		{name: "order not membership", in: OrderPlaced, want: false},
		{name: "empty not membership", in: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsMembershipEvent(tc.in); got != tc.want {
				t.Fatalf("IsMembershipEvent(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestMembershipPayloadValidate(t *testing.T) {
	t.Parallel()
	base := MembershipPayload{
		Version:        MembershipPayloadVersion,
		TenantID:       "tenant-a",
		SubscriptionID: "sub-1",
		MemberID:       "mem-1",
		MemberEmail:    "alice@example.com",
		PlanID:         "plan-1",
		PlanName:       "Pro",
		State:          "active",
	}
	t.Run("ok", func(t *testing.T) {
		t.Parallel()
		if err := base.Validate(); err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
	})
	cases := []struct {
		name   string
		mutate func(*MembershipPayload)
	}{
		{"missing version", func(p *MembershipPayload) { p.Version = 0 }},
		{"missing tenant", func(p *MembershipPayload) { p.TenantID = "" }},
		{"missing subscription", func(p *MembershipPayload) { p.SubscriptionID = "" }},
		{"missing plan", func(p *MembershipPayload) { p.PlanID = "" }},
		{"missing state", func(p *MembershipPayload) { p.State = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := base
			tc.mutate(&p)
			err := p.Validate()
			if err == nil {
				t.Fatalf("expected validation error for %s, got nil", tc.name)
			}
			if !errors.Is(err, ErrMembershipPayloadInvalid) {
				t.Fatalf("expected ErrMembershipPayloadInvalid, got %v", err)
			}
		})
	}
}

func TestNewMembershipEvent(t *testing.T) {
	t.Parallel()
	occurred := time.Date(2026, 5, 8, 17, 30, 0, 0, time.UTC)
	payload := MembershipPayload{
		TenantID:       "tenant-a",
		SubscriptionID: "sub-1",
		MemberID:       "mem-1",
		MemberEmail:    "alice@example.com",
		PlanID:         "plan-1",
		PlanName:       "Pro",
		State:          "trial",
		// Version intentionally zero -- constructor must default.
	}
	cases := []struct {
		name      string
		eventType EventType
		source    string
		expSource string
		wantErr   bool
	}{
		{"created with default source", MembershipCreated, "", "mc-api.membership", false},
		{"renewed with explicit source", MembershipRenewed, "workflow.membership", "workflow.membership", false},
		{"cancelled", MembershipCancelled, "mc-api.membership", "mc-api.membership", false},
		{"paused", MembershipPaused, "mc-api.membership", "mc-api.membership", false},
		{"resumed", MembershipResumed, "mc-api.membership", "mc-api.membership", false},
		{"non-membership rejected", ProductCreated, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			evt, err := NewMembershipEvent(tc.eventType, tc.source, occurred, payload)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %s", tc.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if evt.Type != tc.eventType {
				t.Fatalf("type = %v, want %v", evt.Type, tc.eventType)
			}
			if evt.TenantID != payload.TenantID {
				t.Fatalf("envelope tenant_id = %q, want %q", evt.TenantID, payload.TenantID)
			}
			if evt.Source != tc.expSource {
				t.Fatalf("source = %q, want %q", evt.Source, tc.expSource)
			}
			if !evt.Timestamp.Equal(occurred) {
				t.Fatalf("timestamp = %v, want %v", evt.Timestamp, occurred)
			}
			gotVersion, ok := evt.Payload["version"].(int)
			if !ok || gotVersion != MembershipPayloadVersion {
				t.Fatalf("payload version = %v, want %d", evt.Payload["version"], MembershipPayloadVersion)
			}
			if got := evt.Payload["tenant_id"]; got != payload.TenantID {
				t.Fatalf("payload tenant_id = %v, want %q", got, payload.TenantID)
			}
			// Round-trip JSON to ensure the map[string]any serialises stably.
			data, err := json.Marshal(evt)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var roundtrip Event
			if err := json.Unmarshal(data, &roundtrip); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if roundtrip.Type != evt.Type || roundtrip.TenantID != evt.TenantID {
				t.Fatalf("round-trip mismatch: %+v", roundtrip)
			}
		})
	}
}

func TestNewMembershipEventDefaultsTimestamp(t *testing.T) {
	t.Parallel()
	payload := MembershipPayload{
		Version: MembershipPayloadVersion, TenantID: "t-1",
		SubscriptionID: "sub-1", PlanID: "plan-1", State: "active",
	}
	evt, err := NewMembershipEvent(MembershipCreated, "", time.Time{}, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Timestamp.IsZero() {
		t.Fatalf("expected default timestamp, got zero")
	}
}

func TestNewMembershipEventInvalidPayloadIsRejected(t *testing.T) {
	t.Parallel()
	// PlanID missing -> validation fails before event creation.
	payload := MembershipPayload{TenantID: "t-1", SubscriptionID: "sub-1", State: "active"}
	if _, err := NewMembershipEvent(MembershipCreated, "", time.Now(), payload); err == nil {
		t.Fatalf("expected error from invalid payload")
	} else if !errors.Is(err, ErrMembershipPayloadInvalid) {
		t.Fatalf("expected ErrMembershipPayloadInvalid, got %v", err)
	}
}

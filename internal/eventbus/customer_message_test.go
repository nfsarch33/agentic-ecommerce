package eventbus

import (
	"errors"
	"testing"
	"time"
)

func TestCustomerMessagePayload_Validate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		p     CustomerMessagePayload
		isErr bool
	}{
		{"valid", CustomerMessagePayload{Version: 1, TenantID: "t", MessageID: "m", Channel: "tiktok"}, false},
		{"missing_version", CustomerMessagePayload{TenantID: "t", MessageID: "m", Channel: "tiktok"}, true},
		{"missing_tenant", CustomerMessagePayload{Version: 1, MessageID: "m", Channel: "tiktok"}, true},
		{"missing_message_id", CustomerMessagePayload{Version: 1, TenantID: "t", Channel: "tiktok"}, true},
		{"missing_channel", CustomerMessagePayload{Version: 1, TenantID: "t", MessageID: "m"}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.p.Validate()
			if tc.isErr {
				if !errors.Is(err, ErrCustomerMessagePayloadInvalid) {
					t.Fatalf("err = %v, want ErrCustomerMessagePayloadInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestCustomerMessagePayload_AsMapHasAllKeys(t *testing.T) {
	t.Parallel()
	p := CustomerMessagePayload{
		Version: 1, TenantID: "t", MessageID: "m", Channel: "tiktok",
		ThreadID: "th", BuyerID: "b", Intent: "i", Sentiment: "s",
		Language: "en", ConfidenceScore: 0.9, Outcome: "auto_replied",
		ReplyText: "hi", ProviderMessageID: "p", Reason: "r",
		OccurredAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	}
	m := p.asMap()
	keys := []string{"version", "tenant_id", "message_id", "channel", "thread_id", "buyer_id", "intent", "sentiment", "language", "confidence_score", "outcome", "reply_text", "provider_message_id", "reason", "occurred_at"}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing key %s", k)
		}
	}
}

func TestNewCustomerMessageReceivedEvent_Defaults(t *testing.T) {
	t.Parallel()
	evt, err := NewCustomerMessageReceivedEvent("", time.Time{}, CustomerMessagePayload{
		TenantID: "t", MessageID: "m", Channel: "tiktok",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if evt.Type != CustomerMessageReceived {
		t.Fatalf("Type = %s", evt.Type)
	}
	if evt.Source == "" {
		t.Fatalf("Source empty")
	}
	if evt.Timestamp.IsZero() {
		t.Fatalf("Timestamp zero")
	}
}

func TestNewCustomerMessageRepliedEvent_Defaults(t *testing.T) {
	t.Parallel()
	evt, err := NewCustomerMessageRepliedEvent("test", time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC), CustomerMessagePayload{
		Version: 1, TenantID: "t", MessageID: "m", Channel: "facebook",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if evt.Type != CustomerMessageReplied {
		t.Fatalf("Type = %s", evt.Type)
	}
	if evt.Source != "test" {
		t.Fatalf("Source = %s", evt.Source)
	}
}

func TestNewCustomerMessageEscalatedEvent_RejectsInvalid(t *testing.T) {
	t.Parallel()
	_, err := NewCustomerMessageEscalatedEvent("", time.Time{}, CustomerMessagePayload{})
	if !errors.Is(err, ErrCustomerMessagePayloadInvalid) {
		t.Fatalf("err = %v", err)
	}
}

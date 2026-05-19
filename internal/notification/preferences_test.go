package notification_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/notification"
)

func TestShouldNotify_DefaultTrue(t *testing.T) {
	t.Parallel()
	store := notification.NewPreferenceStore()
	// no preferences set -- defaults to notify
	if !store.ShouldNotify("u1", "email", "order_shipped") {
		t.Fatal("expected default true")
	}
}

func TestShouldNotify_OptOut(t *testing.T) {
	t.Parallel()
	store := notification.NewPreferenceStore()
	store.SetPreference("u1", "email", "order_shipped", false)
	if store.ShouldNotify("u1", "email", "order_shipped") {
		t.Fatal("user opted out; should not notify")
	}
}

func TestShouldNotify_OptIn(t *testing.T) {
	t.Parallel()
	store := notification.NewPreferenceStore()
	store.SetPreference("u1", "sms", "promotions", false)
	store.SetPreference("u1", "sms", "promotions", true)
	if !store.ShouldNotify("u1", "sms", "promotions") {
		t.Fatal("user opted back in; should notify")
	}
}

func TestShouldNotify_ChannelOverride(t *testing.T) {
	t.Parallel()
	store := notification.NewPreferenceStore()
	// opt out from email entirely but keep push enabled
	store.SetChannelDefault("u1", "email", false)
	if store.ShouldNotify("u1", "email", "any_event") {
		t.Fatal("channel-level opt-out should suppress all email events")
	}
	if !store.ShouldNotify("u1", "push", "any_event") {
		t.Fatal("push not disabled; should notify")
	}
}

func TestShouldNotify_EventSpecificOverridesChannel(t *testing.T) {
	t.Parallel()
	store := notification.NewPreferenceStore()
	// channel default off, but one specific event explicitly on
	store.SetChannelDefault("u1", "email", false)
	store.SetPreference("u1", "email", "security_alert", true)
	if !store.ShouldNotify("u1", "email", "security_alert") {
		t.Fatal("event-specific opt-in should override channel default")
	}
}

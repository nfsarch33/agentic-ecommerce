package social

import (
	"errors"
	"testing"
)

func TestValidateChannelCredentials_Instagram(t *testing.T) {
	t.Parallel()
	err := ValidateChannelCredentials("instagram", ChannelConfig{
		AppID:       "ig-app",
		AppSecret:   "secret-at-least-16-chars!!",
		AccessToken: "tok",
	})
	if err != nil {
		t.Fatalf("valid IG creds should pass: %v", err)
	}
}

func TestValidateChannelCredentials_PinterestMissingSecret(t *testing.T) {
	t.Parallel()
	err := ValidateChannelCredentials("pinterest", ChannelConfig{
		AppID:       "pin-app",
		AppSecret:   "",
		AccessToken: "tok",
	})
	if !errors.Is(err, ErrFactoryMissingConfig) {
		t.Fatalf("expected ErrFactoryMissingConfig, got: %v", err)
	}
}

func TestValidateChannelCredentials_UnknownChannel(t *testing.T) {
	t.Parallel()
	err := ValidateChannelCredentials("shopify", ChannelConfig{})
	if !errors.Is(err, ErrUnknownChannel) {
		t.Fatalf("expected ErrUnknownChannel, got: %v", err)
	}
}

func TestValidateChannelCredentials_All6Channels(t *testing.T) {
	t.Parallel()
	for _, ch := range AllChannelNames {
		cfg := ChannelConfig{
			AppID:       "app-" + ch,
			AppSecret:   "secret-long-enough-16b!!",
			AccessToken: "tok-" + ch,
			TenantID:    "t1",
		}
		if err := ValidateChannelCredentials(ch, cfg); err != nil {
			t.Fatalf("channel=%s should validate: %v", ch, err)
		}
	}
}

func TestIsKnownChannel(t *testing.T) {
	t.Parallel()
	for _, ch := range AllChannelNames {
		if !IsKnownChannel(ch) {
			t.Fatalf("%s should be known", ch)
		}
	}
	if IsKnownChannel("shopify") {
		t.Fatal("shopify should not be known")
	}
}

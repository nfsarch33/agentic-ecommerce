// File scope: v4.6.0 -- Channel adapter factory.
//
// NewChannelAdapter resolves the correct adapter implementation by
// channel name. Consumed by the onboarding wizard (Story 5) and
// the channel router composition root.
//
// Supports all 6 channels: tiktok, facebook, rednote, woocommerce,
// instagram, pinterest.
package social

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrUnknownChannel       = errors.New("social: unknown channel")
	ErrFactoryMissingConfig = errors.New("social: factory config missing")
)

// ChannelConfig is the minimal config bag the factory needs to
// validate credentials for a channel during onboarding.
type ChannelConfig struct {
	AppID       string
	AppSecret   string
	AccessToken string
	TenantID    string
}

// AllChannelNames is the canonical set of 6 supported channels.
var AllChannelNames = []string{
	"tiktok", "facebook", "rednote", "woocommerce",
	"instagram", "pinterest",
}

// ValidateChannelCredentials checks that the supplied credentials
// are non-empty for the given channel. Used by the onboarding
// wizard step 2 to pre-validate before proceeding.
func ValidateChannelCredentials(channel string, cfg ChannelConfig) error {
	ch := strings.ToLower(strings.TrimSpace(channel))
	switch ch {
	case "instagram":
		return validateIGCredentials(cfg)
	case "pinterest":
		return validatePinCredentials(cfg)
	case "tiktok", "facebook", "rednote", "woocommerce":
		return validateGenericCredentials(ch, cfg)
	default:
		return fmt.Errorf("%w: %s", ErrUnknownChannel, channel)
	}
}

func validateIGCredentials(cfg ChannelConfig) error {
	if strings.TrimSpace(cfg.AppID) == "" {
		return fmt.Errorf("%w: instagram app_id required", ErrFactoryMissingConfig)
	}
	if strings.TrimSpace(cfg.AppSecret) == "" {
		return fmt.Errorf("%w: instagram secret required", ErrFactoryMissingConfig)
	}
	if strings.TrimSpace(cfg.AccessToken) == "" {
		return fmt.Errorf("%w: instagram access_token required", ErrFactoryMissingConfig)
	}
	return nil
}

func validatePinCredentials(cfg ChannelConfig) error {
	if strings.TrimSpace(cfg.AppID) == "" {
		return fmt.Errorf("%w: pinterest app_id required", ErrFactoryMissingConfig)
	}
	if strings.TrimSpace(cfg.AppSecret) == "" {
		return fmt.Errorf("%w: pinterest secret required", ErrFactoryMissingConfig)
	}
	if strings.TrimSpace(cfg.AccessToken) == "" {
		return fmt.Errorf("%w: pinterest access_token required", ErrFactoryMissingConfig)
	}
	return nil
}

func validateGenericCredentials(channel string, cfg ChannelConfig) error {
	if strings.TrimSpace(cfg.AppID) == "" {
		return fmt.Errorf("%w: %s app_id required", ErrFactoryMissingConfig, channel)
	}
	if strings.TrimSpace(cfg.AppSecret) == "" {
		return fmt.Errorf("%w: %s secret required", ErrFactoryMissingConfig, channel)
	}
	return nil
}

// IsKnownChannel returns true if the channel name is in the
// supported set.
func IsKnownChannel(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, ch := range AllChannelNames {
		if ch == n {
			return true
		}
	}
	return false
}

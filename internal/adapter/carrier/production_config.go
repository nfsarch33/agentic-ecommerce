package carrier

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	envAusPostAPIKey    = "EC_AUSPOST_API_KEY"
	envAusPostAPISecret = "EC_AUSPOST_API_SECRET"
	envDHLAPIKey        = "EC_DHL_API_KEY"
	envDHLAPISecret     = "EC_DHL_API_SECRET"

	auspostProdBaseURL  = "https://digitalapi.auspost.com.au"
	auspostProdOAuthURL = ""

	dhlProdBaseURL  = "https://express.api.dhl.com"
	dhlProdOAuthURL = "https://api.dhl.com/ecs/v1/auth/accesstoken"
)

// ProductionAusPostConfig constructs an AusPostConfig from env vars
// for production use. Returns an error if the required env vars are
// missing.
func ProductionAusPostConfig() (AusPostConfig, error) {
	apiKey := os.Getenv(envAusPostAPIKey)
	if strings.TrimSpace(apiKey) == "" {
		return AusPostConfig{}, fmt.Errorf("%w: %s required", ErrCarrierClientUnconfigured, envAusPostAPIKey)
	}
	apiSecret := os.Getenv(envAusPostAPISecret)
	if strings.TrimSpace(apiSecret) == "" {
		return AusPostConfig{}, fmt.Errorf("%w: %s required", ErrCarrierClientUnconfigured, envAusPostAPISecret)
	}
	return AusPostConfig{
		BaseURL:    auspostProdBaseURL,
		APIKey:     apiKey,
		APISecret:  apiSecret,
		HTTPClient: &http.Client{Timeout: DefaultAusPostTimeout},
		Now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

// ProductionDHLConfig constructs a DHLConfig from env vars for
// production use. Returns an error if the required env vars are
// missing.
func ProductionDHLConfig() (DHLConfig, error) {
	apiKey := os.Getenv(envDHLAPIKey)
	if strings.TrimSpace(apiKey) == "" {
		return DHLConfig{}, fmt.Errorf("%w: %s required", ErrCarrierClientUnconfigured, envDHLAPIKey)
	}
	apiSecret := os.Getenv(envDHLAPISecret)
	if strings.TrimSpace(apiSecret) == "" {
		return DHLConfig{}, fmt.Errorf("%w: %s required", ErrCarrierClientUnconfigured, envDHLAPISecret)
	}
	return DHLConfig{
		BaseURL:      dhlProdBaseURL,
		OAuthURL:     dhlProdOAuthURL,
		ClientID:     apiKey,
		ClientSecret: apiSecret,
		HTTPClient:   &http.Client{Timeout: DefaultDHLTimeout},
		Now:          func() time.Time { return time.Now().UTC() },
	}, nil
}

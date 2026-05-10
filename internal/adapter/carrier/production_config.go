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
	envAusPostSandbox   = "EC_AUSPOST_SANDBOX"
	envDHLAPIKey        = "EC_DHL_API_KEY"
	envDHLAPISecret     = "EC_DHL_API_SECRET"
	envDHLSandbox       = "EC_DHL_SANDBOX"

	auspostProductionURL = "https://digitalapi.auspost.com.au/shipping/v1/"
	auspostSandboxURL    = "https://digitalapi.auspost.com.au/test/shipping/v1/"

	// Legacy alias kept for backward-compat with callers that
	// referenced the old constant before the sandbox toggle existed.
	auspostProdBaseURL  = auspostProductionURL
	auspostProdOAuthURL = ""

	dhlProductionURL = "https://express.api.dhl.com/mydhlapi/"
	dhlSandboxURL    = "https://express.api.dhl.com/mydhlapi/test/"
	dhlProdBaseURL   = dhlProductionURL
	dhlProdOAuthURL  = "https://api.dhl.com/ecs/v1/auth/accesstoken"
)

// ResolveAusPostBaseURL returns the sandbox or production URL based
// on the EC_AUSPOST_SANDBOX env var (default: sandbox).
func ResolveAusPostBaseURL() string {
	v := os.Getenv(envAusPostSandbox)
	if v == "" || v == "true" || v == "1" {
		return auspostSandboxURL
	}
	return auspostProductionURL
}

// ResolveDHLBaseURL returns the sandbox or production URL based on
// the EC_DHL_SANDBOX env var (default: sandbox).
func ResolveDHLBaseURL() string {
	v := os.Getenv(envDHLSandbox)
	if v == "" || v == "true" || v == "1" {
		return dhlSandboxURL
	}
	return dhlProductionURL
}

// ProductionAusPostConfig constructs an AusPostConfig from env vars
// for production use. Returns an error if the required env vars are
// missing. Respects EC_AUSPOST_SANDBOX for URL selection.
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
		BaseURL:    ResolveAusPostBaseURL(),
		APIKey:     apiKey,
		APISecret:  apiSecret,
		HTTPClient: &http.Client{Timeout: DefaultAusPostTimeout},
		Now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

// ProductionDHLConfig constructs a DHLConfig from env vars for
// production use. Returns an error if the required env vars are
// missing. Respects EC_DHL_SANDBOX for URL selection.
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
		BaseURL:      ResolveDHLBaseURL(),
		OAuthURL:     dhlProdOAuthURL,
		ClientID:     apiKey,
		ClientSecret: apiSecret,
		HTTPClient:   &http.Client{Timeout: DefaultDHLTimeout},
		Now:          func() time.Time { return time.Now().UTC() },
	}, nil
}

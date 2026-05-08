package digital

import "errors"

// Sentinel errors returned by the digital bounded context. Adapters and
// handlers wrap these with %w so callers can use errors.Is for typed
// branching.
var (
	// ErrTenantRequired is returned when a tenant id is missing.
	ErrTenantRequired = errors.New("tenant id is required")

	// ErrSKURequired is returned when a digital product SKU is missing.
	ErrSKURequired = errors.New("digital product sku is required")

	// ErrNameRequired is returned when a digital product name is empty.
	ErrNameRequired = errors.New("digital product name is required")

	// ErrFilePathRequired is returned when a digital product file path
	// is empty.
	ErrFilePathRequired = errors.New("digital product file path is required")

	// ErrInvalidFileSize is returned when a file size is non-positive.
	ErrInvalidFileSize = errors.New("digital product file size must be positive")

	// ErrInvalidVersion is returned when a digital product version is
	// empty.
	ErrInvalidVersion = errors.New("digital product version is required")

	// ErrLicenseRevoked is returned when an operation is attempted on
	// a revoked licence.
	ErrLicenseRevoked = errors.New("licence is revoked")

	// ErrLicenseExpired is returned when an operation is attempted on
	// an expired licence.
	ErrLicenseExpired = errors.New("licence is expired")

	// ErrInvalidLicense is returned when a licence key fails HMAC
	// validation.
	ErrInvalidLicense = errors.New("invalid licence key")

	// ErrTokenExpired is returned when a download token has passed its
	// expiry timestamp.
	ErrTokenExpired = errors.New("download token has expired")

	// ErrMaxUsesExceeded is returned when a download token has reached
	// its UsesAllowed cap.
	ErrMaxUsesExceeded = errors.New("download token usage cap exceeded")

	// ErrInvalidLicenseKey is returned when a licence key string cannot
	// be parsed (wrong shape, bad checksum). Equivalent to
	// ErrInvalidLicense for HMAC cases; surfaces parse errors too.
	ErrInvalidLicenseKey = errors.New("licence key is malformed or invalid")

	// ErrTenantMismatch is returned when an operation references a
	// resource from another tenant.
	ErrTenantMismatch = errors.New("tenant mismatch")
)

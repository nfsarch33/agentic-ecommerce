package sourcing

import "errors"

// v3.1.0 EC-1-3 sentinels. Callers branch via errors.Is.
var (
	// ErrSourcingUnconfigured is returned by NewChinaSourcingAgent
	// when a required dependency (clients, pool, publisher, tenant)
	// is missing.
	ErrSourcingUnconfigured = errors.New("sourcing: agent unconfigured")

	// ErrOmniParserUnconfigured is returned when the
	// OMNIPARSER_BRIDGE_URL env var is unset and no override is
	// supplied. Production agents reject; tests pass an explicit URL.
	ErrOmniParserUnconfigured = errors.New("sourcing: omniparser bridge unconfigured")

	// ErrSourcingEmptyKeyword is returned by Run when the request
	// carries an empty keyword.
	ErrSourcingEmptyKeyword = errors.New("sourcing: empty keyword")

	// ErrSourcingClosed is returned by Run after Close.
	ErrSourcingClosed = errors.New("sourcing: agent closed")

	// ErrSourcingFanoutFailed is returned when the workerpool refused
	// a fan-out task. Wraps the underlying workerpool error.
	ErrSourcingFanoutFailed = errors.New("sourcing: client fanout failed")

	// ErrSourcingEmptyResults is returned when the search returned
	// zero candidates across every client (or every candidate was
	// filtered out before the rank step).
	ErrSourcingEmptyResults = errors.New("sourcing: no candidates")
)

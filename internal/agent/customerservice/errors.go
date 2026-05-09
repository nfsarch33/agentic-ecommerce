// File scope: v3.6.0 Epic 8 customer service typed errors.
//
// All sentinels follow the existing repo idiom: package-private
// errors.New + lowercase prefix that includes the package name so
// the operator can grep error logs for "customerservice:".
package customerservice

import "errors"

// EC-8-1 enquiry classifier sentinels.
var (
	// ErrClassifierUnconfigured is returned by NewEnquiryClassifier
	// when a required dependency is missing.
	ErrClassifierUnconfigured = errors.New("customerservice: classifier unconfigured")

	// ErrClassifierClosed is returned by Classify after Close.
	ErrClassifierClosed = errors.New("customerservice: classifier closed")

	// ErrUnsupportedLanguage is returned when the operator forces a
	// language override outside the closed enum.
	ErrUnsupportedLanguage = errors.New("customerservice: unsupported language")

	// ErrLLMUnavailable is the typed sentinel surfaced when both the
	// LLM upstream and the rule-based fallback have failed. Surfaced
	// so the operator can spot a stuck-on-template tenant in logs.
	ErrLLMUnavailable = errors.New("customerservice: llm unavailable")

	// ErrLowConfidence is returned when a classification was
	// produced but the merged confidence dropped below the
	// configured human-handoff threshold (default 0.6 per Epic 8
	// acceptance "below 0.7 confidence -> human handoff" -- the
	// agent uses 0.6 as a conservative implementation floor).
	ErrLowConfidence = errors.New("customerservice: low classification confidence")
)

// EC-8-2 FAQ responder sentinels.
var (
	// ErrFAQResponderUnconfigured is returned by NewFAQResponder
	// when a required dependency is missing.
	ErrFAQResponderUnconfigured = errors.New("customerservice: faq responder unconfigured")

	// ErrFAQResponderClosed is returned by Respond after Close.
	ErrFAQResponderClosed = errors.New("customerservice: faq responder closed")

	// ErrNoFAQMatch is returned when no FAQ entry matched the
	// classified intent + language above the minimum match score.
	// Callers route this to the operator escalation queue.
	ErrNoFAQMatch = errors.New("customerservice: no faq match")

	// ErrFAQResponseTooLong is returned when a generated reply
	// exceeds MaxFAQResponseLength characters. The platform reply
	// surfaces (TikTok, FB) cap reply length; the responder rejects
	// over-budget output rather than silently truncating.
	ErrFAQResponseTooLong = errors.New("customerservice: faq response too long")

	// ErrFAQLLMUnavailable is the typed sentinel surfaced when the
	// rerank-and-rephrase LLM call failed AND the deterministic
	// template fallback was used. Mirrors ErrLLMUnavailable in shape.
	ErrFAQLLMUnavailable = errors.New("customerservice: faq llm unavailable")
)

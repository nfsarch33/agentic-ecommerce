// Package customerservice implements the v3.6.0 Epic 8 customer
// service agents: enquiry intent classification (EC-8-1) and the
// FAQ + product Q&A responder (EC-8-2).
//
// Pipeline (driven by the EC-8-3 inbound message webhook):
//
//	inbound message
//	  -> EnquiryClassifier.Classify   (intent + sentiment + language)
//	  -> FAQResponder.Respond         (auto_replied | suggested | escalated)
//	  -> outbound channel client SendMessage (TikTok / FB)
//	  -> audit log
//
// The classifier follows the v3.2.0 EC-2-1 + v3.4.0 EC-5-1 LLM
// failover pattern: IronClaw-backed LLM first, rule-based regex
// fallback second, deterministic template fallback last. See
// `internal/agent/enrichment/description_failover_test.go` for the
// canonical four-failure-shape table the same pattern runs through.
//
// Resilience pillar (v2.10 baseline):
//
//   - Implements lifecycle.Closer.
//   - No raw goroutines: classification is synchronous; the only
//     concurrency concern is request-level cancellation (honoured
//     via ctx on every code path).
//   - All errors typed + %w-wrapped via this package's sentinels.
//   - Tenant awareness: every request carries the configured
//     TenantID; metrics labels include tenant + channel.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 9-sprint streak v3.1.0..v3.5.1; v3.6.0 = sprint 10
// target):
//
//   - EnquiryClassifier.Classify -> envelope; cyclomatic 3
//   - detectLanguage             -> heuristic + override; cyclomatic 4
//   - runLLM                     -> JSON parse + score; cyclomatic 4
//   - runRuleFallback            -> regex/keyword cascade; cyclomatic 5
//   - mergeResults               -> sentiment + confidence merge; cyclomatic 4
//
// Cite skill: go-clean-architecture (port + adapter -- the
// classifier depends on port.AITextGenerator).
package customerservice

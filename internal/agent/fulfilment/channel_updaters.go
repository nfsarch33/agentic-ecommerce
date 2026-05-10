// File scope: v3.9.0 carry-forward closure -- composition-root
// helpers for wiring the v3.8.0 EC-7-4 ChannelStatusUpdater port
// across all four production adapters.
//
// The concrete updaters live in their own packages so they keep
// the underlying client's package locality:
//   - internal/adapter/social.TikTokStatusUpdater
//   - internal/adapter/social.FacebookStatusUpdater
//   - internal/agent/channel.RedNoteStatusUpdater
//   - internal/adapter/woocommerce.StatusUpdater
//
// To avoid an import cycle (fulfilment <- adapter packages) this
// file does NOT import them. It documents the composition contract
// and provides a small ResolveDefaultChannels variadic helper that
// cmd/* binaries call with concrete updater instances.
//
// Decomposition discipline: one helper, cyclomatic 2.
package fulfilment

// ResolveDefaultChannels is the canonical wiring helper for cmd/*
// binaries that build a StatusPropagator with the four production
// adapters. The adapters MUST already implement
// ChannelStatusUpdater; this helper just deduplicates by
// ChannelName so a misconfigured composition root does not register
// the same channel twice.
//
// Cite skill: go-clean-architecture (composition root pattern).
func ResolveDefaultChannels(updaters ...ChannelStatusUpdater) []ChannelStatusUpdater {
	seen := make(map[string]struct{}, len(updaters))
	out := make([]ChannelStatusUpdater, 0, len(updaters))
	for _, u := range updaters {
		if u == nil {
			continue
		}
		name := u.ChannelName()
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, u)
	}
	return out
}

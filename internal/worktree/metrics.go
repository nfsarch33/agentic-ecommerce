package worktree

// Prometheus metric names for worktree coordination.
// These are string constants consumed by the metrics registration layer;
// the actual prometheus.CounterVec registration lives in the app wiring
// (cmd/ or internal/metrics/) to avoid importing prometheus/client_golang
// into a pure-logic package.
const (
	MetricLockAcquisitions = "ec_worktree_lock_acquisitions_total"
	MetricCleanups         = "ec_worktree_cleanups_total"
	MetricHandoffs         = "ec_worktree_handoffs_total"

	LabelRepo    = "repo"
	LabelOutcome = "outcome"
	LabelReason  = "reason"

	OutcomeAcquired = "acquired"
	OutcomeBlocked  = "blocked"
	OutcomeExpired  = "expired"
	OutcomeReleased = "released"
)

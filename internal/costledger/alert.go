package costledger

import "time"

// FailingLoopThreshold is the failing-call count, for one job inside the
// alert window, that raises the same-day alert (the acceptance loop of 21).
const FailingLoopThreshold = 21

// Failure is one recorded failing model call with its wall-clock stamp
// (cost_ledger.created_at on the PG side).
type Failure struct {
	JobID string
	At    time.Time
}

// AlertOnFailingLoop reports whether any single job accumulated
// FailingLoopThreshold or more failures inside the window ending at now.
// The nightly job scopes rows to the window in SQL, then calls this.
//
// MUTANT guard: TestAlertOnFailingLoop pins 21 in-window failures for one
// job -> true; 20 -> false; 21 spread across two jobs -> false.
func AlertOnFailingLoop(failures []Failure, window time.Duration, now time.Time) bool {
	cutoff := now.Add(-window)
	counts := map[string]int{}
	for _, f := range failures {
		if f.At.Before(cutoff) || f.At.After(now) {
			continue
		}
		counts[f.JobID]++
	}
	for _, n := range counts {
		if n >= FailingLoopThreshold {
			return true
		}
	}
	return false
}

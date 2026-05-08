package billing

import "time"

// testTime is a fixed UTC reference used by helpers that need a
// non-zero time.
func testTime() time.Time { return time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC) }

package marketplace

import "time"

// defaultClock returns the current UTC instant in RFC3339 nanos. We
// keep the registry's clock as a string-emitting func so adapters
// don't need to repeat the formatting boilerplate.
func defaultClock() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

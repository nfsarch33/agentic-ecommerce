package digital

import "crypto/rand"

// cryptoRandReader is split into its own file so the test suite can
// swap a deterministic source via WithRandSource without depending on
// crypto/rand. Production code path: read straight from rand.Reader,
// which is OS-backed (`getrandom` on Linux, `Security.framework` on
// macOS) and never blocks after init.
func cryptoRandReader(b []byte) (int, error) {
	return rand.Read(b)
}

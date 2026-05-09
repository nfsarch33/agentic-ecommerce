package replay

import "os"

// writeFile is the tiny test helper that the recorder_test.go uses
// to plant fixtures. Lives in its own file so the production code
// stays free of test-only helpers.
func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}

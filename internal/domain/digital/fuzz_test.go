// Fuzz harness for the HMAC license-key validator. The contract is
// "must NEVER panic on attacker-controlled key bytes; must always
// return an error rather than crash". Seed corpus is a structurally
// valid key plus malformed shapes (wrong segment counts, wrong lengths,
// embedded control bytes, oversized strings).

package digital

import (
	"strings"
	"testing"
)

const fuzzLicenseSecret = "fuzz-secret-at-least-32-bytes-long-yes"

func newFuzzLicenseGenerator() *HMACLicenseKeyGenerator {
	gen, err := NewHMACLicenseKeyGenerator([]byte(fuzzLicenseSecret))
	if err != nil {
		panic(err)
	}
	return gen
}

func FuzzValidateLicenseKey(f *testing.F) {
	gen := newFuzzLicenseGenerator()

	good, err := gen.Generate("tenant-default", []byte("seed"))
	if err != nil {
		f.Fatalf("Generate: %v", err)
	}

	f.Add("tenant-default", good)
	f.Add("tenant-default", "")
	f.Add("tenant-default", "AAAAA-BBBBB-CCCCC-DDDDD-EEEEEEEE")
	f.Add("tenant-default", "AAAAA-BBBBB-CCCCC-DDDDD-EE")
	f.Add("tenant-default", "AAAAABBBBBCCCCCDDDDDEEEEEEEE")
	f.Add("tenant-default", "AAAA-BBBB-CCCC-DDDD-EEEEEEEE")
	f.Add("", good)
	f.Add("\x00\x01\x02", good)
	f.Add("tenant-default", strings.Repeat("A", 4096))
	f.Add("tenant-default", "AAAAA-BBBBB-CCCCC-DDDDD-eeeeeeee")
	f.Add("tenant-default", "AAAAA--CCCCC-DDDDD-EEEEEEEE")

	f.Fuzz(func(t *testing.T, tenantID string, key string) {
		_ = gen.Validate(tenantID, key)
	})
}

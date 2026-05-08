// Benchmarks for the HMAC license-key generator and validator. Validate
// runs in the request hot path when an end user submits a key during
// download; Generate runs at provisioning time. Both must stay fast and
// allocation-stable so a regression cannot quietly degrade UX.

package digital

import (
	"testing"
)

const benchLicenseSecret = "bench-secret-at-least-32-bytes-long-yes"

func benchLicenseGenerator(b *testing.B) *HMACLicenseKeyGenerator {
	b.Helper()
	gen, err := NewHMACLicenseKeyGenerator([]byte(benchLicenseSecret))
	if err != nil {
		b.Fatalf("NewHMACLicenseKeyGenerator: %v", err)
	}
	return gen
}

func BenchmarkLicenseKeyGenerate(b *testing.B) {
	gen := benchLicenseGenerator(b)
	seed := []byte("bench-seed")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gen.Generate("tenant-default", seed)
	}
}

func BenchmarkLicenseKeyValidate(b *testing.B) {
	gen := benchLicenseGenerator(b)
	key, err := gen.Generate("tenant-default", []byte("bench-seed"))
	if err != nil {
		b.Fatalf("Generate: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gen.Validate("tenant-default", key)
	}
}

package digital

import (
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

// fixedRand returns a deterministic byte stream derived from seed so
// generated keys stay golden across test runs.
func fixedRand(seed []byte) func([]byte) (int, error) {
	h := sha256.Sum256(seed)
	state := h[:]
	return func(b []byte) (int, error) {
		// Stretch the seed by repeated SHA-256 hashing.
		written := 0
		for written < len(b) {
			next := sha256.Sum256(state)
			state = next[:]
			n := copy(b[written:], state)
			written += n
		}
		return len(b), nil
	}
}

func newTestGenerator(t *testing.T, seedTag string) *HMACLicenseKeyGenerator {
	t.Helper()
	g, err := NewHMACLicenseKeyGenerator(
		bytes32("test-secret-32-bytes-of-license-key-1!"),
		WithRandSource(fixedRand([]byte(seedTag))),
	)
	if err != nil {
		t.Fatalf("NewHMACLicenseKeyGenerator: %v", err)
	}
	return g
}

func bytes32(seed string) []byte {
	out := make([]byte, 32)
	copy(out, []byte(seed))
	return out
}

func TestNewHMACLicenseKeyGeneratorRejectsShortSecret(t *testing.T) {
	t.Parallel()
	if _, err := NewHMACLicenseKeyGenerator([]byte("too-short")); err == nil {
		t.Fatal("expected error for short secret")
	}
}

func TestHMACLicenseKeyGeneratorRoundTrip(t *testing.T) {
	t.Parallel()
	g := newTestGenerator(t, "round-trip")
	key, err := g.Generate("tenant-a", []byte("seed"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Count(key, "-") != licenseKeyParts-1 {
		t.Fatalf("key = %q, expected %d separators", key, licenseKeyParts-1)
	}
	if err := g.Validate("tenant-a", key); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestHMACLicenseKeyGeneratorRejectsTamperedKey(t *testing.T) {
	t.Parallel()
	g := newTestGenerator(t, "tamper")
	key, err := g.Generate("tenant-a", []byte("seed"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// Flip a single character in the payload portion.
	idx := strings.Index(key, "-")
	if idx < 1 {
		t.Fatalf("malformed key %q", key)
	}
	tampered := []byte(key)
	if tampered[0] == 'A' {
		tampered[0] = 'B'
	} else {
		tampered[0] = 'A'
	}
	if err := g.Validate("tenant-a", string(tampered)); !errors.Is(err, ErrInvalidLicense) {
		t.Fatalf("Validate(tampered) = %v, want ErrInvalidLicense", err)
	}
}

func TestHMACLicenseKeyGeneratorRejectsCrossTenantReplay(t *testing.T) {
	t.Parallel()
	g := newTestGenerator(t, "cross-tenant")
	key, err := g.Generate("tenant-a", []byte("seed"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// Validating the same key for a different tenant MUST fail.
	if err := g.Validate("tenant-b", key); !errors.Is(err, ErrInvalidLicense) {
		t.Fatalf("Validate(tenant-b) = %v, want ErrInvalidLicense", err)
	}
}

func TestHMACLicenseKeyGeneratorRejectsMalformedKey(t *testing.T) {
	t.Parallel()
	g := newTestGenerator(t, "malformed")
	cases := []string{
		"",
		"NOTAKEY",
		"AAAAA-AAAAA-AAAAA-AAAAA",         // missing checksum group
		"AAAAA-AAAAA-AAAAA-AAAAA-X",       // checksum too short
		"AAAAAA-AAAAA-AAAAA-AAAAA-XXXXXX", // group too long
	}
	for _, key := range cases {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			err := g.Validate("tenant-a", key)
			if !errors.Is(err, ErrInvalidLicenseKey) && !errors.Is(err, ErrInvalidLicense) {
				t.Fatalf("Validate(%q) = %v, want ErrInvalidLicenseKey or ErrInvalidLicense", key, err)
			}
		})
	}
}

func TestHMACLicenseKeyGeneratorEmptyTenantRejected(t *testing.T) {
	t.Parallel()
	g := newTestGenerator(t, "empty-tenant")
	if _, err := g.Generate("", []byte("seed")); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("Generate empty tenant err = %v, want ErrTenantRequired", err)
	}
	if err := g.Validate("", "AAAAA-AAAAA-AAAAA-AAAAA-XXXXXXXX"); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("Validate empty tenant err = %v, want ErrTenantRequired", err)
	}
}

func TestHMACLicenseKeyGeneratorDeterministicWithFixedRand(t *testing.T) {
	t.Parallel()
	// Two generators built with the same seed must produce identical
	// keys -- the property the offline ledger fixtures rely on.
	g1 := newTestGenerator(t, "deterministic")
	g2 := newTestGenerator(t, "deterministic")
	k1, err := g1.Generate("tenant-a", []byte("seed-1"))
	if err != nil {
		t.Fatalf("Generate g1: %v", err)
	}
	k2, err := g2.Generate("tenant-a", []byte("seed-1"))
	if err != nil {
		t.Fatalf("Generate g2: %v", err)
	}
	if k1 != k2 {
		t.Fatalf("deterministic generators produced different keys: %q vs %q", k1, k2)
	}
}

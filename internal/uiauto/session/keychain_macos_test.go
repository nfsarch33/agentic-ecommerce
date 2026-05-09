//go:build darwin

package session

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSessionManager_SaveAndLoadRoundtrip exercises the live macOS
// /usr/bin/security CLI when the test runs on a darwin host. Skips
// on Linux/CI -- the FileManager tests cover the cross-platform
// path. The test cleans up after itself with `security delete-
// generic-password`.
func TestSessionManager_SaveAndLoadRoundtrip(t *testing.T) {
	skipIfNoKeychain(t)
	mgr := newKeychainMgrForTest(t)
	ctx, cancel := newTestContext(t)
	defer cancel()
	tenant, channel := uniqueAccount(t)
	defer cleanupKeychain(t, mgr, tenant, channel)
	want := SessionBlob{UserAgent: "kc-roundtrip/1.0"}
	if err := mgr.SaveSession(ctx, tenant, channel, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := mgr.LoadSession(ctx, tenant, channel)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.UserAgent != want.UserAgent {
		t.Fatalf("ua mismatch: %q vs %q", got.UserAgent, want.UserAgent)
	}
}

func TestSessionManagerKC_LoadMissingReturnsNotFound(t *testing.T) {
	skipIfNoKeychain(t)
	mgr := newKeychainMgrForTest(t)
	ctx, cancel := newTestContext(t)
	defer cancel()
	tenant, channel := uniqueAccount(t)
	defer cleanupKeychain(t, mgr, tenant, channel)
	if _, err := mgr.LoadSession(ctx, tenant, channel); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
}

func TestSessionManagerKC_DeleteRemovesEntry(t *testing.T) {
	skipIfNoKeychain(t)
	mgr := newKeychainMgrForTest(t)
	ctx, cancel := newTestContext(t)
	defer cancel()
	tenant, channel := uniqueAccount(t)
	defer cleanupKeychain(t, mgr, tenant, channel)
	if err := mgr.SaveSession(ctx, tenant, channel, SessionBlob{}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := mgr.DeleteSession(ctx, tenant, channel); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := mgr.LoadSession(ctx, tenant, channel); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound after delete, got %v", err)
	}
}

func TestSessionManagerKC_TenantIsolation(t *testing.T) {
	skipIfNoKeychain(t)
	mgr := newKeychainMgrForTest(t)
	ctx, cancel := newTestContext(t)
	defer cancel()
	tenantA, channel := uniqueAccount(t)
	tenantB := tenantA + "-B"
	defer cleanupKeychain(t, mgr, tenantA, channel)
	defer cleanupKeychain(t, mgr, tenantB, channel)
	if err := mgr.SaveSession(ctx, tenantA, channel, SessionBlob{UserAgent: "for-A"}); err != nil {
		t.Fatalf("save A: %v", err)
	}
	if err := mgr.SaveSession(ctx, tenantB, channel, SessionBlob{UserAgent: "for-B"}); err != nil {
		t.Fatalf("save B: %v", err)
	}
	got, err := mgr.LoadSession(ctx, tenantB, channel)
	if err != nil {
		t.Fatalf("load B: %v", err)
	}
	if got.UserAgent != "for-B" {
		t.Fatalf("tenant A leaked into B: %q", got.UserAgent)
	}
}

func TestSessionManagerKC_ExpiredSessionRejected(t *testing.T) {
	skipIfNoKeychain(t)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	saveMgr := newKeychainMgrAt(t, now.Add(-31*24*time.Hour))
	loadMgr := newKeychainMgrAt(t, now)
	ctx, cancel := newTestContext(t)
	defer cancel()
	tenant, channel := uniqueAccount(t)
	defer cleanupKeychain(t, saveMgr, tenant, channel)
	if err := saveMgr.SaveSession(ctx, tenant, channel, SessionBlob{}); err != nil {
		t.Fatalf("save (old): %v", err)
	}
	if _, err := loadMgr.LoadSession(ctx, tenant, channel); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("want ErrSessionExpired, got %v", err)
	}
}

func TestParseKeychainAccounts_FiltersByServiceAndTenant(t *testing.T) {
	t.Parallel()
	dump := []byte(strings.Join([]string{
		`keychain: "/Users/x/Library/Keychains/login.keychain-db"`,
		`    "svce"<blob>="ec-uiauto"`,
		`    "acct"<blob>="tenant-a:rednote"`,
		``,
		`keychain: "/Users/x/Library/Keychains/login.keychain-db"`,
		`    "svce"<blob>="ec-uiauto"`,
		`    "acct"<blob>="tenant-b:rednote"`,
		``,
		`keychain: "/Users/x/Library/Keychains/login.keychain-db"`,
		`    "svce"<blob>="ec-uiauto"`,
		`    "acct"<blob>="tenant-a:tiktok"`,
		``,
		`keychain: "/Users/x/Library/Keychains/login.keychain-db"`,
		`    "svce"<blob>="other-svc"`,
		`    "acct"<blob>="tenant-a:noisy"`,
		``,
	}, "\n"))
	keys := parseKeychainAccounts(dump, "ec-uiauto", "tenant-a")
	if len(keys) != 2 {
		t.Fatalf("want 2 entries for tenant-a, got %d (%v)", len(keys), keys)
	}
	gotChannels := map[string]bool{keys[0].Channel: true, keys[1].Channel: true}
	if !gotChannels["rednote"] || !gotChannels["tiktok"] {
		t.Fatalf("unexpected channels: %v", keys)
	}
}

// helpers ------------------------------------------------------------------

func skipIfNoKeychain(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(KeychainBinary); err != nil {
		t.Skipf("skipping: %s unavailable: %v", KeychainBinary, err)
	}
}

func newKeychainMgrForTest(t *testing.T) *KeychainManager {
	t.Helper()
	return newKeychainMgrAt(t, time.Now().UTC())
}

func newKeychainMgrAt(t *testing.T, now time.Time) *KeychainManager {
	t.Helper()
	mgr, err := NewKeychainManager(KeychainManagerConfig{
		MasterKey: newTestKey(t, 0x42),
		TenantTag: "test-" + strings.ReplaceAll(t.Name(), "/", "_"),
		Binary:    KeychainBinary,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new keychain manager: %v", err)
	}
	return mgr
}

func uniqueAccount(t *testing.T) (string, string) {
	t.Helper()
	return fmt.Sprintf("v370-%d", time.Now().UnixNano()), "rednote"
}

func cleanupKeychain(t *testing.T, mgr *KeychainManager, tenant, channel string) {
	t.Helper()
	ctx, cancel := newTestContext(t)
	defer cancel()
	if err := mgr.DeleteSession(ctx, tenant, channel); err != nil && !errors.Is(err, ErrSessionNotFound) {
		t.Logf("cleanup warning: %v", err)
	}
}

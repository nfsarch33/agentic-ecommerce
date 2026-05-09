package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionManagerFile_AESGCMRoundtrip(t *testing.T) {
	t.Parallel()
	mgr := newFileMgr(t, time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))
	ctx, cancel := newTestContext(t)
	defer cancel()
	want := SessionBlob{
		Cookies: []map[string]any{{"name": "x", "value": "y"}},
		LocalStorage: map[string]string{
			"k": "v",
		},
		UserAgent: "test/2.0",
	}
	if err := mgr.SaveSession(ctx, "tenant-a", "rednote", want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := mgr.LoadSession(ctx, "tenant-a", "rednote")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.UserAgent != want.UserAgent {
		t.Fatalf("ua mismatch: %q vs %q", got.UserAgent, want.UserAgent)
	}
	if v := got.LocalStorage["k"]; v != "v" {
		t.Fatalf("localStorage k missing: %v", got.LocalStorage)
	}
}

func TestSessionManager_LoadMissingReturnsNotFound(t *testing.T) {
	t.Parallel()
	mgr := newFileMgr(t, time.Now().UTC())
	ctx, cancel := newTestContext(t)
	defer cancel()
	if _, err := mgr.LoadSession(ctx, "tenant-a", "rednote"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
}

func TestSessionManager_DeleteRemovesEntry(t *testing.T) {
	t.Parallel()
	mgr := newFileMgr(t, time.Now().UTC())
	ctx, cancel := newTestContext(t)
	defer cancel()
	if err := mgr.SaveSession(ctx, "tenant-a", "rednote", SessionBlob{}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := mgr.DeleteSession(ctx, "tenant-a", "rednote"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := mgr.LoadSession(ctx, "tenant-a", "rednote"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound after delete, got %v", err)
	}
}

func TestSessionManager_TenantIsolation(t *testing.T) {
	t.Parallel()
	mgr := newFileMgr(t, time.Now().UTC())
	ctx, cancel := newTestContext(t)
	defer cancel()
	if err := mgr.SaveSession(ctx, "tenant-a", "rednote", SessionBlob{UserAgent: "for-a"}); err != nil {
		t.Fatalf("save tenant a: %v", err)
	}
	if err := mgr.SaveSession(ctx, "tenant-b", "rednote", SessionBlob{UserAgent: "for-b"}); err != nil {
		t.Fatalf("save tenant b: %v", err)
	}
	bs, err := mgr.LoadSession(ctx, "tenant-b", "rednote")
	if err != nil {
		t.Fatalf("load b: %v", err)
	}
	if bs.UserAgent != "for-b" {
		t.Fatalf("tenant a leaked into b: %q", bs.UserAgent)
	}
	if _, err := mgr.LoadSession(ctx, "tenant-a", "tiktok"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound for unrecorded channel, got %v", err)
	}
}

func TestSessionManager_ExpiredSessionRejected(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	old := now.Add(-31 * 24 * time.Hour)
	saveMgr := newFileMgrAt(t, old)
	loadMgr := newFileMgrSharing(t, saveMgr.root, now)
	ctx, cancel := newTestContext(t)
	defer cancel()
	if err := saveMgr.SaveSession(ctx, "tenant-a", "rednote", SessionBlob{}); err != nil {
		t.Fatalf("save (old): %v", err)
	}
	if _, err := loadMgr.LoadSession(ctx, "tenant-a", "rednote"); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("want ErrSessionExpired, got %v", err)
	}
}

func TestSessionManagerFile_TamperDetected(t *testing.T) {
	t.Parallel()
	mgr := newFileMgr(t, time.Now().UTC())
	ctx, cancel := newTestContext(t)
	defer cancel()
	if err := mgr.SaveSession(ctx, "tenant-a", "rednote", SessionBlob{UserAgent: "tamperme"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	target := filepath.Join(mgr.root, "tenant-a", "rednote.enc")
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read for tamper: %v", err)
	}
	raw[len(raw)-1] ^= 0x01
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		t.Fatalf("write tamper: %v", err)
	}
	if _, err := mgr.LoadSession(ctx, "tenant-a", "rednote"); !errors.Is(err, ErrSessionTampered) {
		t.Fatalf("want ErrSessionTampered, got %v", err)
	}
}

func TestSessionManagerFile_ListSessions(t *testing.T) {
	t.Parallel()
	mgr := newFileMgr(t, time.Now().UTC())
	ctx, cancel := newTestContext(t)
	defer cancel()
	for _, ch := range []string{"rednote", "tiktok"} {
		if err := mgr.SaveSession(ctx, "tenant-a", ch, SessionBlob{}); err != nil {
			t.Fatalf("save %s: %v", ch, err)
		}
	}
	keys, err := mgr.ListSessions(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("want 2 keys, got %d (%v)", len(keys), keys)
	}
	got := map[string]bool{}
	for _, k := range keys {
		if k.TenantID != "tenant-a" {
			t.Fatalf("wrong tenant in list: %v", k)
		}
		got[k.Channel] = true
	}
	if !got["rednote"] || !got["tiktok"] {
		t.Fatalf("unexpected keys: %v", got)
	}
	// list of unknown tenant returns empty without error.
	other, err := mgr.ListSessions(ctx, "tenant-z")
	if err != nil {
		t.Fatalf("list unknown: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("unknown tenant should list empty, got %v", other)
	}
}

func TestSessionManagerFile_FromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(MasterKeyEnvVar, strings.Repeat("12", 32))
	mgr, err := NewFileManagerFromEnv(dir)
	if err != nil {
		t.Fatalf("from env: %v", err)
	}
	if mgr.root != dir {
		t.Fatalf("root mismatch: %s vs %s", mgr.root, dir)
	}
}

func TestSessionManagerFile_FromEnvRejectsMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(MasterKeyEnvVar, "")
	_, err := NewFileManagerFromEnv(dir)
	if !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("want ErrInvalidMasterKey, got %v", err)
	}
}

// helpers ------------------------------------------------------------------

func newFileMgr(t *testing.T, now time.Time) *FileManager {
	t.Helper()
	return newFileMgrAt(t, now)
}

func newFileMgrAt(t *testing.T, now time.Time) *FileManager {
	t.Helper()
	dir := t.TempDir()
	mgr, err := NewFileManager(FileManagerConfig{
		Root:      dir,
		MasterKey: newTestKey(t, 0x42),
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new file manager: %v", err)
	}
	return mgr
}

func newFileMgrSharing(t *testing.T, root string, now time.Time) *FileManager {
	t.Helper()
	mgr, err := NewFileManager(FileManagerConfig{
		Root:      root,
		MasterKey: newTestKey(t, 0x42),
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new file manager: %v", err)
	}
	return mgr
}

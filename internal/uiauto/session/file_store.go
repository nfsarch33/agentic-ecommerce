// File scope: cross-platform pure-Go AES-256-GCM file-based
// SessionManager fallback. This is the default on Linux + WSL +
// Windows + CI runners (which is Ubuntu) so the test suite can
// exercise the encryption + tamper-detection paths without
// platform-specific tooling.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4): SaveSession + LoadSession route through the helpers from
// manager.go (serialize/encrypt/write and read/decrypt/deserialize)
// so per-function cyclomatic stays under 6.
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileManager is the pure-Go AES-256-GCM file-based SessionManager.
// Used everywhere the OS keychain is unavailable (Linux + WSL +
// Windows + Ubuntu CI runners). Files live at <root>/<tenant>/<chan>.enc.
type FileManager struct {
	root      string
	masterKey []byte
	now       func() time.Time
	logger    *slog.Logger

	mu sync.Mutex
}

// FileManagerConfig wires the file-based manager. Empty Now defaults
// to time.Now (UTC).
type FileManagerConfig struct {
	Root      string // session files live under <Root>/<tenant_id>/<channel>.enc
	MasterKey []byte // 32-byte AES-256 key (caller already decoded)
	Now       func() time.Time
	Logger    *slog.Logger
}

// NewFileManager constructs a file-based SessionManager. Root is
// created if missing.
func NewFileManager(cfg FileManagerConfig) (*FileManager, error) {
	if strings.TrimSpace(cfg.Root) == "" {
		return nil, fmt.Errorf("session: file manager root required")
	}
	if len(cfg.MasterKey) != MasterKeyBytes {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrInvalidMasterKey, len(cfg.MasterKey), MasterKeyBytes)
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if err := os.MkdirAll(cfg.Root, 0o700); err != nil {
		return nil, fmt.Errorf("session: mkdir root: %w", err)
	}
	return &FileManager{
		root:      cfg.Root,
		masterKey: append([]byte(nil), cfg.MasterKey...),
		now:       cfg.Now,
		logger:    cfg.Logger,
	}, nil
}

// NewFileManagerFromEnv reads EC_SESSION_MASTER_KEY and constructs a
// file-based manager rooted at root. Convenience wrapper for the
// composition root.
func NewFileManagerFromEnv(root string) (*FileManager, error) {
	key, err := resolveMasterKey(os.Getenv(MasterKeyEnvVar))
	if err != nil {
		return nil, err
	}
	return NewFileManager(FileManagerConfig{Root: root, MasterKey: key})
}

// SaveSession writes an encrypted blob to disk.
func (f *FileManager) SaveSession(ctx context.Context, tenantID, channel string, blob SessionBlob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateKey(tenantID, channel); err != nil {
		return err
	}
	payload, err := serialize(blob, f.now())
	if err != nil {
		return err
	}
	cipherText, err := encrypt(f.masterKey, payload)
	if err != nil {
		return err
	}
	return f.writeBlob(tenantID, channel, cipherText)
}

// writeBlob is the disk-write half of SaveSession. Atomic
// rename-on-write so a crash mid-write cannot leave a half-rewritten
// file.
func (f *FileManager) writeBlob(tenantID, channel string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	dir := filepath.Join(f.root, tenantID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("session: mkdir %s: %w", dir, err)
	}
	final := filepath.Join(dir, channel+".enc")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return fmt.Errorf("session: write tmp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("session: rename: %w", err)
	}
	return nil
}

// LoadSession reads, decrypts, and decodes the blob. Returns
// ErrSessionNotFound if the file is missing, ErrSessionExpired if
// the recorded_at is older than MaxSessionAge, or ErrSessionTampered
// if the AES-GCM auth fails.
func (f *FileManager) LoadSession(ctx context.Context, tenantID, channel string) (SessionBlob, error) {
	if err := ctx.Err(); err != nil {
		return SessionBlob{}, err
	}
	if err := validateKey(tenantID, channel); err != nil {
		return SessionBlob{}, err
	}
	cipherText, err := f.readBlob(tenantID, channel)
	if err != nil {
		return SessionBlob{}, err
	}
	plaintext, err := decrypt(f.masterKey, cipherText)
	if err != nil {
		return SessionBlob{}, err
	}
	blob, err := deserialize(plaintext)
	if err != nil {
		return SessionBlob{}, err
	}
	if err := checkExpiry(blob.RecordedAt, f.now(), MaxSessionAge); err != nil {
		return SessionBlob{}, err
	}
	return blob, nil
}

// readBlob is the disk-read half of LoadSession.
func (f *FileManager) readBlob(tenantID, channel string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	final := filepath.Join(f.root, tenantID, channel+".enc")
	data, err := os.ReadFile(final)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s/%s", ErrSessionNotFound, tenantID, channel)
		}
		return nil, fmt.Errorf("session: read: %w", err)
	}
	return data, nil
}

// DeleteSession removes the file (no-op if missing).
func (f *FileManager) DeleteSession(ctx context.Context, tenantID, channel string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateKey(tenantID, channel); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	final := filepath.Join(f.root, tenantID, channel+".enc")
	if err := os.Remove(final); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s/%s", ErrSessionNotFound, tenantID, channel)
		}
		return fmt.Errorf("session: remove: %w", err)
	}
	return nil
}

// ListSessions returns all stored channels for tenantID.
func (f *FileManager) ListSessions(ctx context.Context, tenantID string) ([]ChannelKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidTenantID
	}
	if strings.Contains(tenantID, channelKeySeparator) {
		return nil, fmt.Errorf("%w: must not contain %q", ErrInvalidTenantID, channelKeySeparator)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	dir := filepath.Join(f.root, tenantID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("session: list dir: %w", err)
	}
	out := make([]ChannelKey, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".enc") {
			continue
		}
		channel := strings.TrimSuffix(name, ".enc")
		out = append(out, ChannelKey{TenantID: tenantID, Channel: channel})
	}
	return out, nil
}

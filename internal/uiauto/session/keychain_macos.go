//go:build darwin

// File scope: macOS-only Keychain-backed SessionManager. Uses the
// /usr/bin/security CLI per the existing runx pattern (cited in the
// no-shell-leak rule). The blob is encrypted-at-rest with AES-256-
// GCM BEFORE being placed in the keychain (defense in depth).
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4): SaveSession + LoadSession route through the helpers from
// manager.go (serialize/encrypt/keychainWrite and read/decrypt/
// deserialize) so per-function cyclomatic stays under 6.
package session

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// KeychainServicePrefix is the `-s` value used for every uiauto
// session entry. The full account is `<tenant_id>:<channel>`.
const KeychainServicePrefix = "ec-uiauto"

// KeychainBinary is the CLI used to drive the macOS keychain. We
// bake in the absolute path for the no-shell-leak audit and to
// eliminate PATH ambiguity. Override via SECURITY_BIN env var only
// in tests.
const KeychainBinary = "/usr/bin/security"

// keychainNotFoundExitCode is the exit status `security find-generic
// -password` returns when the entry is missing. macOS-specific.
const keychainNotFoundExitCode = 44

// KeychainManager is the macOS-only SessionManager backed by the
// /usr/bin/security CLI. Encrypts blobs with AES-256-GCM BEFORE
// stashing them in the keychain.
type KeychainManager struct {
	masterKey []byte
	tenant    string
	binary    string
	now       func() time.Time
	logger    *slog.Logger

	mu sync.Mutex
}

// KeychainManagerConfig wires the macOS keychain manager.
type KeychainManagerConfig struct {
	MasterKey []byte
	TenantTag string // subdivision of the keychain (typically "default")
	Binary    string // override for tests; defaults to KeychainBinary
	Now       func() time.Time
	Logger    *slog.Logger
}

// NewKeychainManager constructs a keychain-backed manager.
func NewKeychainManager(cfg KeychainManagerConfig) (*KeychainManager, error) {
	if len(cfg.MasterKey) != MasterKeyBytes {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrInvalidMasterKey, len(cfg.MasterKey), MasterKeyBytes)
	}
	if cfg.Binary == "" {
		cfg.Binary = KeychainBinary
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if _, err := os.Stat(cfg.Binary); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrKeychainUnavailable, cfg.Binary, err)
	}
	return &KeychainManager{
		masterKey: append([]byte(nil), cfg.MasterKey...),
		tenant:    cfg.TenantTag,
		binary:    cfg.Binary,
		now:       cfg.Now,
		logger:    cfg.Logger,
	}, nil
}

// service returns the keychain service label for this manager. We
// suffix with the optional tenant tag so co-tenanted machines do
// not collide.
func (k *KeychainManager) service() string {
	if k.tenant == "" {
		return KeychainServicePrefix
	}
	return KeychainServicePrefix + "-" + k.tenant
}

// SaveSession marshals + encrypts + writes via `security add-
// generic-password`.
func (k *KeychainManager) SaveSession(ctx context.Context, tenantID, channel string, blob SessionBlob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateKey(tenantID, channel); err != nil {
		return err
	}
	payload, err := serialize(blob, k.now())
	if err != nil {
		return err
	}
	cipherText, err := encrypt(k.masterKey, payload)
	if err != nil {
		return err
	}
	return k.keychainWrite(ctx, tenantID, channel, cipherText)
}

// keychainWrite shells out to security add-generic-password. The
// blob is hex-encoded so the CLI's argument-passing rules don't
// mangle binary bytes. -U updates an existing entry.
func (k *KeychainManager) keychainWrite(ctx context.Context, tenantID, channel string, cipherText []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	account := composeAccount(tenantID, channel)
	encoded := hex.EncodeToString(cipherText)
	cmd := exec.CommandContext(ctx, k.binary,
		"add-generic-password",
		"-U",
		"-s", k.service(),
		"-a", account,
		"-w", encoded,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: write: %v: %s", ErrKeychainUnavailable, err, stderr.String())
	}
	return nil
}

// LoadSession reads + decrypts + deserializes via `security find-
// generic-password -gw` (the `-gw` combo prints the password to
// stderr ... or is it stdout depending on macOS version; see
// keychainRead).
func (k *KeychainManager) LoadSession(ctx context.Context, tenantID, channel string) (SessionBlob, error) {
	if err := ctx.Err(); err != nil {
		return SessionBlob{}, err
	}
	if err := validateKey(tenantID, channel); err != nil {
		return SessionBlob{}, err
	}
	encoded, err := k.keychainRead(ctx, tenantID, channel)
	if err != nil {
		return SessionBlob{}, err
	}
	cipherText, err := hex.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return SessionBlob{}, fmt.Errorf("%w: hex decode keychain payload: %v", ErrSessionTampered, err)
	}
	plaintext, err := decrypt(k.masterKey, cipherText)
	if err != nil {
		return SessionBlob{}, err
	}
	blob, err := deserialize(plaintext)
	if err != nil {
		return SessionBlob{}, err
	}
	if err := checkExpiry(blob.RecordedAt, k.now(), MaxSessionAge); err != nil {
		return SessionBlob{}, err
	}
	return blob, nil
}

// keychainRead shells out to `security find-generic-password -w`,
// which prints just the password (the encrypted blob hex) to
// stdout. The standard `security` CLI quirk: with `-g` it prints
// to stderr; with `-w` it prints to stdout. We use `-w` to keep
// the parsing trivial.
func (k *KeychainManager) keychainRead(ctx context.Context, tenantID, channel string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	account := composeAccount(tenantID, channel)
	cmd := exec.CommandContext(ctx, k.binary,
		"find-generic-password",
		"-w",
		"-s", k.service(),
		"-a", account,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == keychainNotFoundExitCode {
			return "", fmt.Errorf("%w: %s/%s", ErrSessionNotFound, tenantID, channel)
		}
		return "", fmt.Errorf("%w: read: %v: %s", ErrKeychainUnavailable, err, stderr.String())
	}
	return stdout.String(), nil
}

// DeleteSession shells out to `security delete-generic-password`.
func (k *KeychainManager) DeleteSession(ctx context.Context, tenantID, channel string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateKey(tenantID, channel); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	account := composeAccount(tenantID, channel)
	cmd := exec.CommandContext(ctx, k.binary,
		"delete-generic-password",
		"-s", k.service(),
		"-a", account,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == keychainNotFoundExitCode {
			return fmt.Errorf("%w: %s/%s", ErrSessionNotFound, tenantID, channel)
		}
		return fmt.Errorf("%w: delete: %v: %s", ErrKeychainUnavailable, err, stderr.String())
	}
	return nil
}

// ListSessions enumerates keychain entries by service. Uses
// `security dump-keychain` filtered by service prefix; this is the
// supported macOS pattern for projection-style queries.
//
// For tenant scoping we filter to accounts that begin with
// "<tenantID>:".
func (k *KeychainManager) ListSessions(ctx context.Context, tenantID string) ([]ChannelKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidTenantID
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	cmd := exec.CommandContext(ctx, k.binary, "dump-keychain")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: dump: %v: %s", ErrKeychainUnavailable, err, stderr.String())
	}
	return parseKeychainAccounts(stdout.Bytes(), k.service(), tenantID), nil
}

// parseKeychainAccounts is split out so the regex / heuristic stays
// testable without shelling out. Parses the `dump-keychain` output
// for entries whose `svce` matches `service` and whose account
// prefix matches `tenantID:`.
func parseKeychainAccounts(dump []byte, service, tenantID string) []ChannelKey {
	prefix := tenantID + channelKeySeparator
	var out []ChannelKey
	var lastSvc, lastAcct string
	for _, line := range strings.Split(string(dump), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "\"svce\"<blob>="):
			lastSvc = unquoteKeychainBlob(line)
		case strings.HasPrefix(line, "\"acct\"<blob>="):
			lastAcct = unquoteKeychainBlob(line)
		case line == "" || strings.HasPrefix(line, "keychain:"):
			if lastSvc == service && strings.HasPrefix(lastAcct, prefix) {
				_, channel, _ := decomposeAccount(lastAcct)
				out = append(out, ChannelKey{TenantID: tenantID, Channel: channel})
			}
			lastSvc, lastAcct = "", ""
		}
	}
	if lastSvc == service && strings.HasPrefix(lastAcct, prefix) {
		_, channel, _ := decomposeAccount(lastAcct)
		out = append(out, ChannelKey{TenantID: tenantID, Channel: channel})
	}
	return out
}

// unquoteKeychainBlob extracts the value from a `dump-keychain` line
// of the form `"svce"<blob>="ec-uiauto"`. Falls back to the raw RHS
// if the value is not double-quoted (some macOS releases drop the
// quotes).
func unquoteKeychainBlob(line string) string {
	idx := strings.Index(line, "=")
	if idx < 0 || idx == len(line)-1 {
		return ""
	}
	rhs := strings.TrimSpace(line[idx+1:])
	if len(rhs) >= 2 && rhs[0] == '"' && rhs[len(rhs)-1] == '"' {
		return rhs[1 : len(rhs)-1]
	}
	return rhs
}

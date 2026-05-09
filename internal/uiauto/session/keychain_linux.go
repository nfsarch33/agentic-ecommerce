//go:build linux

// File scope: Linux Keychain shim. No external dependency added in
// v3.7.0 -- on Linux the operator falls back to the pure-Go
// AES-256-GCM file store (NewFileManagerFromEnv). A future sprint
// can swap this for github.com/zalando/go-keyring (D-Bus secret
// service) once we have a Linux deployment lane that actually has
// gnome-keyring or kwallet running.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4): trivially passthroughs, all real work happens in the
// FileManager.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// ErrLinuxKeychainNotImplemented is the typed sentinel returned by
// every Linux keychain entry point so callers can detect the
// unimplemented state and route to NewFileManagerFromEnv.
var ErrLinuxKeychainNotImplemented = errors.New("session: linux keychain not implemented; use NewFileManagerFromEnv")

// LinuxKeychainManager is a placeholder so cross-platform tests can
// reference the type. All methods return ErrLinuxKeychainNotImplemented.
type LinuxKeychainManager struct{}

// NewLinuxKeychainManager returns a stub manager. v3.7.0 ships
// without D-Bus binding -- file fallback is the supported lane.
func NewLinuxKeychainManager() (*LinuxKeychainManager, error) {
	return nil, ErrLinuxKeychainNotImplemented
}

// SaveSession returns ErrLinuxKeychainNotImplemented.
func (l *LinuxKeychainManager) SaveSession(_ context.Context, _, _ string, _ SessionBlob) error {
	return ErrLinuxKeychainNotImplemented
}

// LoadSession returns ErrLinuxKeychainNotImplemented.
func (l *LinuxKeychainManager) LoadSession(_ context.Context, _, _ string) (SessionBlob, error) {
	return SessionBlob{}, ErrLinuxKeychainNotImplemented
}

// DeleteSession returns ErrLinuxKeychainNotImplemented.
func (l *LinuxKeychainManager) DeleteSession(_ context.Context, _, _ string) error {
	return ErrLinuxKeychainNotImplemented
}

// ListSessions returns ErrLinuxKeychainNotImplemented.
func (l *LinuxKeychainManager) ListSessions(_ context.Context, _ string) ([]ChannelKey, error) {
	return nil, ErrLinuxKeychainNotImplemented
}

// DefaultLinuxFileRoot returns the canonical path for the file-based
// session store on Linux per the FreeDesktop XDG Base Directory
// Specification. Falls back to ~/.config/agentic-ecommerce/sessions
// when XDG_CONFIG_HOME is unset.
func DefaultLinuxFileRoot() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return fmt.Sprintf("%s/agentic-ecommerce/sessions", dir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("session: resolve home: %w", err)
	}
	return fmt.Sprintf("%s/.config/agentic-ecommerce/sessions", home), nil
}

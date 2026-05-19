package objectstore

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// File scope: targeted coverage for previously-uncovered branches in
// LocalStore Put/Open/Delete and the helper key/path normalisation
// functions.

func TestLocalStorePutRejectsNilBody(t *testing.T) {
	t.Parallel()

	store, err := New(Config{Provider: ProviderLocal, RootDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := store.Put(context.Background(), port.MediaObject{Key: "p/img.png"}); !errors.Is(err, ErrMissingObjectBody) {
		t.Fatalf("err = %v, want ErrMissingObjectBody", err)
	}
}

func TestLocalStorePutPropagatesContextCancellation(t *testing.T) {
	t.Parallel()

	store, err := New(Config{Provider: ProviderLocal, RootDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Put(ctx, port.MediaObject{
		Key:  "p/img.png",
		Body: strings.NewReader("data"),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestLocalStoreOpenAndDeletePropagateContextCancellation(t *testing.T) {
	t.Parallel()

	store, err := New(Config{Provider: ProviderLocal, RootDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Open(ctx, "any-key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open err = %v, want context.Canceled", err)
	}
	if err := store.Delete(ctx, "any-key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete err = %v, want context.Canceled", err)
	}
}

func TestLocalStoreOpenReturnsErrorForMissingFile(t *testing.T) {
	t.Parallel()

	store, err := New(Config{Provider: ProviderLocal, RootDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := store.Open(context.Background(), "missing/key.png"); err == nil {
		t.Fatal("expected error opening missing file")
	}
}

func TestLocalStoreDeleteIsIdempotentForMissingObject(t *testing.T) {
	t.Parallel()

	store, err := New(Config{Provider: ProviderLocal, RootDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.Delete(context.Background(), "never-stored.png"); err != nil {
		t.Fatalf("Delete missing key err = %v, want nil (idempotent)", err)
	}
}

func TestLocalStoreObjectURLDefaultsToBareKeyWhenPublicBaseURLBlank(t *testing.T) {
	t.Parallel()

	store := NewLocalStore(Config{Provider: ProviderLocal, RootDir: t.TempDir()})
	saved, err := store.Put(context.Background(), port.MediaObject{
		Key:  "products/p1/raw.bin",
		Body: strings.NewReader("x"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if saved.URL != saved.Key {
		t.Fatalf("URL = %q, want bare key %q", saved.URL, saved.Key)
	}
}

func TestLocalStoreApplyPrefixWhenConfigured(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := NewLocalStore(Config{
		Provider:      ProviderLocal,
		RootDir:       root,
		PublicBaseURL: "/media",
		Prefix:        "tenant-1",
	})
	saved, err := store.Put(context.Background(), port.MediaObject{
		Key:  "products/p1/img.png",
		Body: strings.NewReader("png"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !strings.HasPrefix(saved.Key, "tenant-1/") {
		t.Fatalf("saved key = %q, want tenant-1/ prefix", saved.Key)
	}

	target := filepath.Join(root, filepath.FromSlash(saved.Key))
	if !strings.HasSuffix(target, filepath.FromSlash("tenant-1/products/p1/img.png")) {
		t.Fatalf("target path = %q, want prefix applied", target)
	}
}

func TestCleanObjectKeyRejectsEmptyAndAbsoluteKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "whitespace only", input: "   "},
		{name: "absolute root", input: "/root.png"},
		{name: "parent traversal segment", input: "products/../etc/passwd"},
		{name: "just dotdot", input: ".."},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := cleanObjectKey(tc.input); !errors.Is(err, ErrInvalidObjectKey) {
				t.Fatalf("cleanObjectKey(%q) err = %v, want ErrInvalidObjectKey", tc.input, err)
			}
		})
	}
}

func TestCleanObjectKeyNormalisesSeparatorsAndCleansPath(t *testing.T) {
	t.Parallel()

	got, err := cleanObjectKey("products\\p1\\image.png")
	if err != nil {
		t.Fatalf("cleanObjectKey: %v", err)
	}
	if got != "products/p1/image.png" {
		t.Fatalf("normalised key = %q, want products/p1/image.png", got)
	}
}

func TestCleanPrefixHandlesEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, input, want string
	}{
		{name: "trims surrounding slashes", input: "/tenant-1/", want: "tenant-1"},
		{name: "normalises backslashes", input: "tenant\\region", want: "tenant/region"},
		{name: "rejects bare dot", input: ".", want: ""},
		{name: "rejects bare dotdot", input: "..", want: ""},
		{name: "rejects parent traversal", input: "tenant/../escape", want: ""},
		{name: "blank returns empty", input: "  ", want: ""},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cleanPrefix(tc.input); got != tc.want {
				t.Fatalf("cleanPrefix(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestLocalStoreOpenReadsBodyBackThroughIo confirms the Open ReadCloser
// can fully drain a multi-chunk write via standard io helpers, defending
// against accidental double-buffer regressions in the future.
func TestLocalStoreOpenReadsBodyBackThroughIo(t *testing.T) {
	t.Parallel()

	store, err := New(Config{Provider: ProviderLocal, RootDir: t.TempDir(), PublicBaseURL: "/media"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := strings.Repeat("x", 4096)
	saved, err := store.Put(context.Background(), port.MediaObject{
		Key:         "p/large.bin",
		ContentType: "application/octet-stream",
		Body:        strings.NewReader(want),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, err := store.Open(context.Background(), saved.Key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != want {
		t.Fatalf("body length = %d, want %d", len(got), len(want))
	}
}

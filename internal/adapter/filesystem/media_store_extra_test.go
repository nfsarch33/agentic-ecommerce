package filesystem

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// File scope: targeted coverage for previously-uncovered branches in the
// filesystem MediaStore: nil body, context cancellation, missing-file
// reads, idempotent deletes, and the URL helper default.

func TestMediaStorePutRejectsNilBody(t *testing.T) {
	t.Parallel()

	store := NewMediaStore(Config{RootDir: t.TempDir()})
	if _, err := store.Put(context.Background(), port.MediaObject{Key: "p/img.png"}); !errors.Is(err, ErrMissingMediaBody) {
		t.Fatalf("err = %v, want ErrMissingMediaBody", err)
	}
}

func TestMediaStorePutPropagatesContextCancellation(t *testing.T) {
	t.Parallel()

	store := NewMediaStore(Config{RootDir: t.TempDir()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Put(ctx, port.MediaObject{Key: "p/img.png", Body: strings.NewReader("x")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestMediaStoreOpenAndDeletePropagateContextCancellation(t *testing.T) {
	t.Parallel()

	store := NewMediaStore(Config{RootDir: t.TempDir()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Open(ctx, "any-key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open err = %v, want context.Canceled", err)
	}
	if err := store.Delete(ctx, "any-key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete err = %v, want context.Canceled", err)
	}
}

func TestMediaStoreOpenReturnsErrorForMissingFile(t *testing.T) {
	t.Parallel()

	store := NewMediaStore(Config{RootDir: t.TempDir()})
	if _, err := store.Open(context.Background(), "missing.png"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestMediaStoreDeleteIsIdempotent(t *testing.T) {
	t.Parallel()

	store := NewMediaStore(Config{RootDir: t.TempDir()})
	if err := store.Delete(context.Background(), "missing.png"); err != nil {
		t.Fatalf("Delete missing key err = %v, want nil", err)
	}
}

func TestMediaStoreObjectURLDefaultsToBareKeyWhenPublicBaseURLBlank(t *testing.T) {
	t.Parallel()

	store := NewMediaStore(Config{RootDir: t.TempDir()})
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

func TestMediaStoreCleanObjectKeyRejectsTraversalAndColon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, input string
	}{
		{name: "empty", input: ""},
		{name: "whitespace", input: "   "},
		{name: "absolute path", input: "/root.png"},
		{name: "parent traversal", input: "../escape.png"},
		{name: "drive-letter colon", input: "C:/secret.png"},
		{name: "bare dotdot", input: ".."},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := cleanObjectKey(tc.input); !errors.Is(err, ErrInvalidMediaKey) {
				t.Fatalf("cleanObjectKey(%q) err = %v, want ErrInvalidMediaKey", tc.input, err)
			}
		})
	}
}

func TestMediaStorePutOpenRoundTripWithPublicBaseURL(t *testing.T) {
	t.Parallel()

	store := NewMediaStore(Config{RootDir: t.TempDir(), PublicBaseURL: "/files/"})
	saved, err := store.Put(context.Background(), port.MediaObject{
		Key:         "products/p1/raw.bin",
		ContentType: "application/octet-stream",
		Body:        strings.NewReader("hello"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if saved.URL != "/files/products/p1/raw.bin" {
		t.Fatalf("URL = %q, want trailing slash trimmed", saved.URL)
	}

	rc, err := store.Open(context.Background(), saved.Key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q", string(body))
	}
}

func TestNewMediaStoreFallsBackToDefaultRootDir(t *testing.T) {
	t.Parallel()

	store := NewMediaStore(Config{RootDir: ""})
	if store.rootDir != ".local/media-uploads" {
		t.Fatalf("rootDir = %q, want default .local/media-uploads", store.rootDir)
	}
}

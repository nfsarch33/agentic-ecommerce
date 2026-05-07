package filesystem

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

func TestMediaStorePutOpenDeleteRoundTrip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := NewMediaStore(Config{RootDir: root, PublicBaseURL: "/media"})

	saved, err := store.Put(context.Background(), port.MediaObject{
		Key:         "products/p1/original.webp",
		ContentType: "image/webp",
		Body:        strings.NewReader("webp-data"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if saved.Key != "products/p1/original.webp" {
		t.Fatalf("Key = %q", saved.Key)
	}
	if saved.ContentType != "image/webp" || saved.SizeBytes != int64(len("webp-data")) {
		t.Fatalf("metadata = %+v", saved)
	}
	if saved.URL != "/media/products/p1/original.webp" {
		t.Fatalf("URL = %q", saved.URL)
	}

	onDisk, err := os.ReadFile(filepath.Join(root, "products", "p1", "original.webp"))
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if string(onDisk) != "webp-data" {
		t.Fatalf("stored bytes = %q", string(onDisk))
	}

	reader, err := store.Open(context.Background(), saved.Key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read opened object: %v", err)
	}
	if string(got) != "webp-data" {
		t.Fatalf("opened bytes = %q", string(got))
	}

	if err := store.Delete(context.Background(), saved.Key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "products", "p1", "original.webp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stored file still exists or unexpected error: %v", err)
	}
}

func TestMediaStoreRejectsUnsafeKeys(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := NewMediaStore(Config{RootDir: root})

	for _, key := range []string{"", "../outside.webp", "/absolute.webp", "products/../outside.webp", "products/../../outside.webp"} {
		t.Run(key, func(t *testing.T) {
			_, err := store.Put(context.Background(), port.MediaObject{
				Key:         key,
				ContentType: "image/webp",
				Body:        strings.NewReader("x"),
			})
			if !errors.Is(err, ErrInvalidMediaKey) {
				t.Fatalf("Put err = %v, want ErrInvalidMediaKey", err)
			}
		})
	}
}

func TestMediaStoreRequiresBody(t *testing.T) {
	t.Parallel()

	store := NewMediaStore(Config{RootDir: t.TempDir()})
	_, err := store.Put(context.Background(), port.MediaObject{
		Key:         "products/p1/original.webp",
		ContentType: "image/webp",
	})
	if !errors.Is(err, ErrMissingMediaBody) {
		t.Fatalf("Put err = %v, want ErrMissingMediaBody", err)
	}
}

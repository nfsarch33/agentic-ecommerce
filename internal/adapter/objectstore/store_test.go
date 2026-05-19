package objectstore

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

func TestLocalStorePutOpenDeleteRoundTrip(t *testing.T) {
	t.Parallel()

	store, err := New(Config{Provider: ProviderLocal, RootDir: t.TempDir(), PublicBaseURL: "/media"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	saved, err := store.Put(context.Background(), port.MediaObject{
		Key:         "products/p1/original.png",
		ContentType: "image/png",
		Body:        strings.NewReader("png-data"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if saved.URL != "/media/products/p1/original.png" || saved.SizeBytes != int64(len("png-data")) {
		t.Fatalf("stored metadata = %+v", saved)
	}

	reader, err := store.Open(context.Background(), saved.Key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "png-data" {
		t.Fatalf("body = %q", string(body))
	}
	if err := store.Delete(context.Background(), saved.Key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestLocalStoreRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := New(Config{Provider: ProviderLocal, RootDir: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = store.Put(context.Background(), port.MediaObject{
		Key:         "../escape.png",
		ContentType: "image/png",
		Body:        strings.NewReader("x"),
	})
	if !errors.Is(err, ErrInvalidObjectKey) {
		t.Fatalf("Put err = %v, want ErrInvalidObjectKey", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "..", "escape.png")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unexpected escaped file: %v", statErr)
	}
}

func TestCloudStoreStubsAreExplicitlyUnavailable(t *testing.T) {
	t.Parallel()

	for _, provider := range []Provider{ProviderS3, ProviderGCS} {
		t.Run(string(provider), func(t *testing.T) {
			t.Parallel()
			store, err := New(Config{Provider: provider, Bucket: "media-bucket", Region: "ap-southeast-2"})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = store.Put(context.Background(), port.MediaObject{
				Key:         "products/p1/original.png",
				ContentType: "image/png",
				Body:        strings.NewReader("png-data"),
			})
			if !errors.Is(err, ErrCloudObjectStoreNotConfigured) {
				t.Fatalf("Put err = %v, want ErrCloudObjectStoreNotConfigured", err)
			}
			if _, err := store.Open(context.Background(), "products/p1/original.png"); !errors.Is(err, ErrCloudObjectStoreNotConfigured) {
				t.Fatalf("Open err = %v, want ErrCloudObjectStoreNotConfigured", err)
			}
			if err := store.Delete(context.Background(), "products/p1/original.png"); !errors.Is(err, ErrCloudObjectStoreNotConfigured) {
				t.Fatalf("Delete err = %v, want ErrCloudObjectStoreNotConfigured", err)
			}
		})
	}
}

func TestNewRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{Provider: Provider("azure")}); err == nil {
		t.Fatal("New err = nil, want unknown provider error")
	}
}

package storage_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/storage"
)

func cfg() storage.UploadConfig {
	return storage.UploadConfig{
		MaxSizeBytes: 1024,
		AllowedMIMEs: []string{"image/jpeg", "image/png"},
		CDNBaseURL:   "https://cdn.test.com",
	}
}

func TestUpload_StoresFile(t *testing.T) {
	t.Parallel()
	fs := storage.NewFileStore(cfg())
	meta := storage.FileMeta{Name: "a.jpg", MIMEType: "image/jpeg", SizeBytes: 100}
	rec, err := fs.Upload(strings.NewReader("data"), meta)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if rec.ID == "" {
		t.Fatal("expected file ID")
	}
}

func TestUpload_ValidateRejectsOversized(t *testing.T) {
	t.Parallel()
	fs := storage.NewFileStore(cfg())
	meta := storage.FileMeta{Name: "big.jpg", MIMEType: "image/jpeg", SizeBytes: 9999}
	if err := fs.Validate(meta); err == nil {
		t.Fatal("expected oversized error")
	}
}

func TestUpload_ValidateRejectsBadMIME(t *testing.T) {
	t.Parallel()
	fs := storage.NewFileStore(cfg())
	meta := storage.FileMeta{Name: "doc.pdf", MIMEType: "application/pdf", SizeBytes: 100}
	if err := fs.Validate(meta); err == nil {
		t.Fatal("expected invalid MIME error")
	}
}

func TestUpload_ResizeCreatesVariant(t *testing.T) {
	t.Parallel()
	fs := storage.NewFileStore(cfg())
	meta := storage.FileMeta{Name: "a.jpg", MIMEType: "image/jpeg", SizeBytes: 100}
	rec, _ := fs.Upload(strings.NewReader("img"), meta)
	resized, err := fs.Resize(rec.ID, 200, 200)
	if err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if resized.ID == rec.ID {
		t.Fatal("expected new ID for resized variant")
	}
}

func TestUpload_CDNURLFormat(t *testing.T) {
	t.Parallel()
	fs := storage.NewFileStore(cfg())
	url := fs.CDNUrl("FILE-0001")
	if url != "https://cdn.test.com/FILE-0001" {
		t.Fatalf("unexpected CDN URL: %s", url)
	}
}

func TestUpload_CleanupRemovesOldFiles(t *testing.T) {
	t.Parallel()
	fs := storage.NewFileStore(cfg())
	meta := storage.FileMeta{Name: "old.jpg", MIMEType: "image/jpeg", SizeBytes: 10}
	fs.Upload(strings.NewReader("data"), meta)
	time.Sleep(5 * time.Millisecond)
	removed, err := fs.Cleanup(time.Millisecond)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
}

package storage_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/storage"
)

func TestArchive_CreateManifest(t *testing.T) {
	t.Parallel()
	a := storage.NewArchiver()
	records := []storage.Record{{ID: "r1", Data: []byte("data"), CreatedAt: time.Now()}}
	m, err := a.Archive(nil, records, "s3://bucket/archive")
	if err != nil {
		t.Fatalf("archive failed: %v", err)
	}
	if m.ID == "" {
		t.Fatal("expected non-empty manifest ID")
	}
	if m.RecordCount != 1 {
		t.Fatalf("expected record count 1, got %d", m.RecordCount)
	}
}

func TestArchive_RestoreRetrievesRecords(t *testing.T) {
	t.Parallel()
	a := storage.NewArchiver()
	records := []storage.Record{{ID: "r1", Data: []byte("hello")}}
	m, _ := a.Archive(nil, records, "dest")
	restored, err := a.Restore(nil, m.ID)
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if len(restored) != 1 || restored[0].ID != "r1" {
		t.Fatalf("expected record r1, got %v", restored)
	}
}

func TestArchive_RestoreNonExistentError(t *testing.T) {
	t.Parallel()
	a := storage.NewArchiver()
	_, err := a.Restore(nil, "nonexistent")
	if err != storage.ErrManifestNotFound {
		t.Fatalf("expected ErrManifestNotFound, got %v", err)
	}
}

func TestArchive_RetentionPolicyEnforced(t *testing.T) {
	t.Parallel()
	a := storage.NewArchiver()
	records := []storage.Record{{ID: "r1"}}
	m, _ := a.Archive(nil, records, "dest")
	// Pretend it was created 10 days ago by using a future "now"
	policy := storage.RetentionPolicy{MaxAge: 5 * 24 * time.Hour}
	removed := a.EnforceRetention(policy, time.Now().Add(10*24*time.Hour))
	found := false
	for _, id := range removed {
		if id == m.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected manifest to be removed by retention policy")
	}
}

func TestArchive_CompressDecompressRoundtrip(t *testing.T) {
	t.Parallel()
	original := []byte("hello world this is test data for compression")
	compressed, err := storage.Compress(original)
	if err != nil {
		t.Fatalf("compress failed: %v", err)
	}
	decompressed, err := storage.Decompress(compressed)
	if err != nil {
		t.Fatalf("decompress failed: %v", err)
	}
	if string(decompressed) != string(original) {
		t.Fatalf("roundtrip mismatch: got %s", decompressed)
	}
}

func TestArchive_EmptyRecords(t *testing.T) {
	t.Parallel()
	a := storage.NewArchiver()
	m, err := a.Archive(nil, nil, "dest")
	if err != nil {
		t.Fatalf("archive empty failed: %v", err)
	}
	if m.RecordCount != 0 {
		t.Fatalf("expected 0 records, got %d", m.RecordCount)
	}
}

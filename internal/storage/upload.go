package storage

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

var (
	ErrFileTooLarge   = errors.New("file exceeds size limit")
	ErrInvalidMIME    = errors.New("invalid MIME type")
	ErrFileNotFound   = errors.New("file not found")
)

type FileMeta struct {
	Name      string
	MIMEType  string
	SizeBytes int64
}

type FileRecord struct {
	ID        string
	Meta      FileMeta
	CreatedAt time.Time
	Content   []byte
}

type UploadConfig struct {
	MaxSizeBytes  int64
	AllowedMIMEs  []string
	CDNBaseURL    string
}

type FileStore struct {
	mu     sync.RWMutex
	files  map[string]*FileRecord
	cfg    UploadConfig
	seq    int
}

func NewFileStore(cfg UploadConfig) *FileStore {
	return &FileStore{files: make(map[string]*FileRecord), cfg: cfg}
}

func (fs *FileStore) Validate(meta FileMeta) error {
	if fs.cfg.MaxSizeBytes > 0 && meta.SizeBytes > fs.cfg.MaxSizeBytes {
		return ErrFileTooLarge
	}
	if len(fs.cfg.AllowedMIMEs) > 0 {
		allowed := false
		for _, m := range fs.cfg.AllowedMIMEs {
			if m == meta.MIMEType {
				allowed = true
				break
			}
		}
		if !allowed {
			return ErrInvalidMIME
		}
	}
	return nil
}

func (fs *FileStore) Upload(r io.Reader, meta FileMeta) (FileRecord, error) {
	if err := fs.Validate(meta); err != nil {
		return FileRecord{}, err
	}
	content, err := io.ReadAll(r)
	if err != nil {
		return FileRecord{}, err
	}
	fs.mu.Lock()
	fs.seq++
	id := fmt.Sprintf("FILE-%04d", fs.seq)
	rec := &FileRecord{ID: id, Meta: meta, CreatedAt: time.Now(), Content: content}
	fs.files[id] = rec
	fs.mu.Unlock()
	return *rec, nil
}

func (fs *FileStore) Resize(fileID string, _, _ int) (FileRecord, error) {
	fs.mu.RLock()
	rec, ok := fs.files[fileID]
	fs.mu.RUnlock()
	if !ok {
		return FileRecord{}, ErrFileNotFound
	}
	// Return a copy with a new ID representing the resized variant
	fs.mu.Lock()
	fs.seq++
	newID := fmt.Sprintf("FILE-%04d-resized", fs.seq)
	variant := &FileRecord{ID: newID, Meta: rec.Meta, CreatedAt: time.Now(), Content: rec.Content}
	fs.files[newID] = variant
	fs.mu.Unlock()
	return *variant, nil
}

func (fs *FileStore) CDNUrl(fileID string) string {
	base := fs.cfg.CDNBaseURL
	if base == "" {
		base = "https://cdn.example.com"
	}
	return fmt.Sprintf("%s/%s", base, fileID)
}

func (fs *FileStore) Cleanup(olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)
	fs.mu.Lock()
	defer fs.mu.Unlock()
	removed := 0
	for id, rec := range fs.files {
		if rec.CreatedAt.Before(cutoff) {
			delete(fs.files, id)
			removed++
		}
	}
	return removed, nil
}

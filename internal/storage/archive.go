package storage

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

var (
	ErrManifestNotFound = errors.New("archive: manifest not found")
	ErrRetentionViolation = errors.New("archive: record exceeds retention policy")
)

type Record struct {
	ID        string
	Data      []byte
	CreatedAt time.Time
}

type ArchiveManifest struct {
	ID          string
	Destination string
	RecordCount int
	CreatedAt   time.Time
}

type RetentionPolicy struct {
	MaxAge  time.Duration
	MaxSize int64
}

type Archiver struct {
	mu        sync.Mutex
	manifests map[string]ArchiveManifest
	archives  map[string][]Record
	seq       int
}

func NewArchiver() *Archiver {
	return &Archiver{
		manifests: make(map[string]ArchiveManifest),
		archives:  make(map[string][]Record),
	}
}

func (a *Archiver) Archive(_ interface{}, records []Record, destination string) (ArchiveManifest, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	id := fmt.Sprintf("manifest-%d", a.seq)
	m := ArchiveManifest{
		ID:          id,
		Destination: destination,
		RecordCount: len(records),
		CreatedAt:   time.Now(),
	}
	a.manifests[id] = m
	cp := make([]Record, len(records))
	copy(cp, records)
	a.archives[id] = cp
	return m, nil
}

func (a *Archiver) Restore(_ interface{}, manifestID string) ([]Record, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	records, ok := a.archives[manifestID]
	if !ok {
		return nil, ErrManifestNotFound
	}
	cp := make([]Record, len(records))
	copy(cp, records)
	return cp, nil
}

func (a *Archiver) EnforceRetention(policy RetentionPolicy, now time.Time) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var removed []string
	for id, m := range a.manifests {
		if policy.MaxAge > 0 && now.Sub(m.CreatedAt) > policy.MaxAge {
			delete(a.manifests, id)
			delete(a.archives, id)
			removed = append(removed, id)
		}
	}
	return removed
}

func Compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func Decompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

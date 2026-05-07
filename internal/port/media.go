package port

import (
	"context"
	"io"
	"time"
)

// MediaStore abstracts object storage so local development can use the
// filesystem while production can switch to an object-store adapter later.
type MediaStore interface {
	Put(ctx context.Context, object MediaObject) (StoredMediaObject, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

type MediaObject struct {
	Key         string
	ContentType string
	Body        io.Reader
}

type StoredMediaObject struct {
	Key         string
	URL         string
	ContentType string
	SizeBytes   int64
	StoredAt    time.Time
}

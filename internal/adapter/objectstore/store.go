package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

var (
	ErrInvalidObjectKey              = errors.New("invalid object key")
	ErrMissingObjectBody             = errors.New("missing object body")
	ErrCloudObjectStoreNotConfigured = errors.New("cloud object store adapter is not configured")
)

type Provider string

const (
	ProviderLocal Provider = "local"
	ProviderS3    Provider = "s3"
	ProviderGCS   Provider = "gcs"
)

type Store = port.MediaStore

type Config struct {
	Provider      Provider
	RootDir       string
	PublicBaseURL string
	Bucket        string
	Region        string
	Endpoint      string
	Prefix        string
}

func New(cfg Config) (Store, error) {
	switch cfg.Provider {
	case "", ProviderLocal:
		return NewLocalStore(cfg), nil
	case ProviderS3, ProviderGCS:
		return NewCloudStub(cfg), nil
	default:
		return nil, fmt.Errorf("unknown object store provider %q", cfg.Provider)
	}
}

type LocalStore struct {
	rootDir       string
	publicBaseURL string
	prefix        string
}

func NewLocalStore(cfg Config) *LocalStore {
	root := strings.TrimSpace(cfg.RootDir)
	if root == "" {
		root = ".local/media-uploads"
	}
	return &LocalStore{
		rootDir:       root,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
		prefix:        cleanPrefix(cfg.Prefix),
	}
}

func (s *LocalStore) Put(ctx context.Context, object port.MediaObject) (port.StoredMediaObject, error) {
	if err := ctx.Err(); err != nil {
		return port.StoredMediaObject{}, err
	}
	if object.Body == nil {
		return port.StoredMediaObject{}, ErrMissingObjectBody
	}
	key, target, err := s.objectPath(object.Key)
	if err != nil {
		return port.StoredMediaObject{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return port.StoredMediaObject{}, fmt.Errorf("create object directory: %w", err)
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return port.StoredMediaObject{}, fmt.Errorf("create object: %w", err)
	}
	size, copyErr := io.Copy(file, object.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return port.StoredMediaObject{}, fmt.Errorf("write object: %w", copyErr)
	}
	if closeErr != nil {
		return port.StoredMediaObject{}, fmt.Errorf("close object: %w", closeErr)
	}
	if err := ctx.Err(); err != nil {
		return port.StoredMediaObject{}, err
	}
	return port.StoredMediaObject{
		Key:         key,
		URL:         s.objectURL(key),
		ContentType: object.ContentType,
		SizeBytes:   size,
		StoredAt:    time.Now().UTC(),
	}, nil
}

func (s *LocalStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, target, err := s.objectPath(key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, fmt.Errorf("open object: %w", err)
	}
	return file, nil
}

func (s *LocalStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, target, err := s.objectPath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

func (s *LocalStore) objectPath(rawKey string) (string, string, error) {
	key, err := cleanObjectKey(path.Join(s.prefix, rawKey))
	if err != nil {
		return "", "", err
	}
	root, err := filepath.Abs(s.rootDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve object root: %w", err)
	}
	target := filepath.Join(root, filepath.FromSlash(key))
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", "", fmt.Errorf("resolve object path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", ErrInvalidObjectKey
	}
	return key, target, nil
}

func (s *LocalStore) objectURL(key string) string {
	if s.publicBaseURL == "" {
		return key
	}
	return s.publicBaseURL + "/" + key
}

type CloudStub struct {
	provider Provider
	bucket   string
	region   string
	endpoint string
	prefix   string
}

func NewCloudStub(cfg Config) *CloudStub {
	return &CloudStub{
		provider: cfg.Provider,
		bucket:   strings.TrimSpace(cfg.Bucket),
		region:   strings.TrimSpace(cfg.Region),
		endpoint: strings.TrimSpace(cfg.Endpoint),
		prefix:   cleanPrefix(cfg.Prefix),
	}
}

func (s *CloudStub) Put(context.Context, port.MediaObject) (port.StoredMediaObject, error) {
	return port.StoredMediaObject{}, ErrCloudObjectStoreNotConfigured
}

func (s *CloudStub) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, ErrCloudObjectStoreNotConfigured
}

func (s *CloudStub) Delete(context.Context, string) error {
	return ErrCloudObjectStoreNotConfigured
}

func cleanObjectKey(raw string) (string, error) {
	key := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if key == "" || strings.HasPrefix(key, "/") {
		return "", ErrInvalidObjectKey
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == ".." {
			return "", ErrInvalidObjectKey
		}
	}
	clean := path.Clean(key)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrInvalidObjectKey
	}
	return clean, nil
}

func cleanPrefix(raw string) string {
	prefix := strings.Trim(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"), "/")
	if prefix == "." || prefix == ".." || strings.Contains(prefix, "../") {
		return ""
	}
	return prefix
}

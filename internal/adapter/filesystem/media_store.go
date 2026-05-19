package filesystem

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

	"github.com/nfsarch33/helixon-ec/internal/port"
)

var (
	ErrInvalidMediaKey  = errors.New("invalid media key")
	ErrMissingMediaBody = errors.New("missing media body")
)

type Config struct {
	RootDir       string
	PublicBaseURL string
}

type MediaStore struct {
	rootDir       string
	publicBaseURL string
}

func NewMediaStore(cfg Config) *MediaStore {
	root := strings.TrimSpace(cfg.RootDir)
	if root == "" {
		root = ".local/media-uploads"
	}
	return &MediaStore{
		rootDir:       root,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
	}
}

func (s *MediaStore) Put(ctx context.Context, object port.MediaObject) (port.StoredMediaObject, error) {
	if err := ctx.Err(); err != nil {
		return port.StoredMediaObject{}, err
	}
	if object.Body == nil {
		return port.StoredMediaObject{}, ErrMissingMediaBody
	}
	key, target, err := s.objectPath(object.Key)
	if err != nil {
		return port.StoredMediaObject{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return port.StoredMediaObject{}, fmt.Errorf("create media directory: %w", err)
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return port.StoredMediaObject{}, fmt.Errorf("create media object: %w", err)
	}
	size, copyErr := io.Copy(file, object.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return port.StoredMediaObject{}, fmt.Errorf("write media object: %w", copyErr)
	}
	if closeErr != nil {
		return port.StoredMediaObject{}, fmt.Errorf("close media object: %w", closeErr)
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

func (s *MediaStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, target, err := s.objectPath(key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, fmt.Errorf("open media object: %w", err)
	}
	return file, nil
}

func (s *MediaStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, target, err := s.objectPath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete media object: %w", err)
	}
	return nil
}

func (s *MediaStore) objectPath(rawKey string) (string, string, error) {
	key, err := cleanObjectKey(rawKey)
	if err != nil {
		return "", "", err
	}
	root, err := filepath.Abs(s.rootDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve media root: %w", err)
	}
	target := filepath.Join(root, filepath.FromSlash(key))
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", "", fmt.Errorf("resolve media object: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", ErrInvalidMediaKey
	}
	return key, target, nil
}

func cleanObjectKey(raw string) (string, error) {
	key := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if key == "" || strings.HasPrefix(key, "/") {
		return "", ErrInvalidMediaKey
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == ".." || strings.Contains(segment, ":") {
			return "", ErrInvalidMediaKey
		}
	}
	clean := path.Clean(key)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrInvalidMediaKey
	}
	return clean, nil
}

func (s *MediaStore) objectURL(key string) string {
	if s.publicBaseURL == "" {
		return key
	}
	return s.publicBaseURL + "/" + key
}

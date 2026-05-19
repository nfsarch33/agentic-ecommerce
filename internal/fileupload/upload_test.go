package fileupload_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/fileupload"
)

func strReader(s string) io.Reader { return strings.NewReader(s) }

func TestHandler_Upload_Success(t *testing.T) {
	t.Parallel()
	store := fileupload.NewMemoryStorage("https://cdn.example.com")
	h := fileupload.NewHandler(fileupload.Config{
		AllowedTypes: []string{"image/png"},
		Storage:      store,
	})
	req := fileupload.UploadRequest{
		Filename:    "photo.png",
		ContentType: "image/png",
		Size:        4,
		Reader:      strReader("data"),
	}
	res, err := h.Upload(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Key != "photo.png" {
		t.Fatalf("want key photo.png, got %q", res.Key)
	}
	if !strings.HasPrefix(res.URL, "https://cdn.example.com") {
		t.Fatalf("unexpected URL: %s", res.URL)
	}
}

func TestHandler_Upload_TooLarge(t *testing.T) {
	t.Parallel()
	store := fileupload.NewMemoryStorage("https://cdn.example.com")
	h := fileupload.NewHandler(fileupload.Config{MaxSize: 10, Storage: store})
	req := fileupload.UploadRequest{Filename: "big.bin", ContentType: "application/octet-stream", Size: 100, Reader: strReader("x")}
	_, err := h.Upload(context.Background(), req)
	if !errors.Is(err, fileupload.ErrFileTooLarge) {
		t.Fatalf("want ErrFileTooLarge, got %v", err)
	}
}

func TestHandler_Upload_UnsupportedType(t *testing.T) {
	t.Parallel()
	store := fileupload.NewMemoryStorage("https://cdn.example.com")
	h := fileupload.NewHandler(fileupload.Config{AllowedTypes: []string{"image/png"}, Storage: store})
	req := fileupload.UploadRequest{Filename: "file.exe", ContentType: "application/x-msdownload", Size: 1, Reader: strReader("x")}
	_, err := h.Upload(context.Background(), req)
	if !errors.Is(err, fileupload.ErrUnsupportedType) {
		t.Fatalf("want ErrUnsupportedType, got %v", err)
	}
}

func TestHandler_Upload_VirusScanBlocks(t *testing.T) {
	t.Parallel()
	store := fileupload.NewMemoryStorage("https://cdn.example.com")
	scan := func(_ context.Context, name string, _ io.Reader) error {
		if strings.HasSuffix(name, ".exe") {
			return errors.New("malware detected")
		}
		return nil
	}
	h := fileupload.NewHandler(fileupload.Config{VirusScanStub: scan, Storage: store})
	req := fileupload.UploadRequest{Filename: "evil.exe", ContentType: "application/octet-stream", Size: 1, Reader: strReader("x")}
	_, err := h.Upload(context.Background(), req)
	if !errors.Is(err, fileupload.ErrVirusScanFailed) {
		t.Fatalf("want ErrVirusScanFailed, got %v", err)
	}
}

func TestMemoryStorage_GetAfterPut(t *testing.T) {
	t.Parallel()
	store := fileupload.NewMemoryStorage("https://cdn.example.com")
	url, err := store.Put(context.Background(), "file.txt", strReader("hello"), 5, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if url == "" {
		t.Fatal("expected non-empty URL")
	}
	data, ok := store.Get("file.txt")
	if !ok {
		t.Fatal("expected stored file")
	}
	if string(data) != "hello" {
		t.Fatalf("want hello, got %q", string(data))
	}
}

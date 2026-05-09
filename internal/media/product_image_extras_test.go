package media

import (
	"context"
	"errors"
	"image/color"
	"strings"
	"testing"
	"time"
)

func TestProductImage_RecordsMetricsHook(t *testing.T) {
	t.Parallel()

	src := newTestPNG(t, 4, 4, color.RGBA{R: 30, G: 60, B: 200, A: 255}, color.RGBA{R: 220, G: 30, B: 30, A: 255})
	dl := &fakeDownloader{
		responses: map[string][]byte{"https://x/x.png": src},
	}
	store := newInMemoryMediaStore()

	var hookCalls int
	pipeline, err := NewProductImagePipeline(nil, ProductImagePipelineConfig{
		Downloader: dl,
		Remover:    NewStubBackgroundRemover(),
		Store:      store,
		TenantID:   "cylrl",
		MetricsHook: func(action Action, status string, duration time.Duration, bytesIn, bytesOut int) {
			hookCalls++
			if action != ActionBackgroundRemoval {
				t.Errorf("unexpected action: %s", action)
			}
		},
	})
	if err != nil {
		t.Fatalf("NewProductImagePipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })

	if _, err := pipeline.Process(context.Background(), ProductImageRequest{ProductID: "x", ImageURL: "https://x/x.png"}); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if hookCalls != 1 {
		t.Fatalf("hookCalls = %d, want 1", hookCalls)
	}
}

func TestProductImage_TooLargeReturnsErrImageTooLarge(t *testing.T) {
	t.Parallel()

	huge := make([]byte, MaxLocalDecodeBytes+1)
	dl := &fakeDownloader{responses: map[string][]byte{"https://x/big.png": huge}}
	store := newInMemoryMediaStore()
	pipeline, err := NewProductImagePipeline(nil, ProductImagePipelineConfig{
		Downloader: dl,
		Remover:    NewStubBackgroundRemover(),
		Store:      store,
		TenantID:   "cylrl",
	})
	if err != nil {
		t.Fatalf("NewProductImagePipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })

	_, err = pipeline.Process(context.Background(), ProductImageRequest{ProductID: "x", ImageURL: "https://x/big.png"})
	if !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("error = %v, want ErrImageTooLarge", err)
	}
}

func TestStubBackgroundRemover_RejectsBadImage(t *testing.T) {
	t.Parallel()

	r := NewStubBackgroundRemover()
	if _, err := r.Remove(context.Background(), []byte("not an image"), "image/png"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestProductImage_KeyPrefixApplied(t *testing.T) {
	t.Parallel()

	src := newTestPNG(t, 4, 4, color.RGBA{R: 30, G: 60, B: 200, A: 255}, color.RGBA{R: 220, G: 30, B: 30, A: 255})
	dl := &fakeDownloader{responses: map[string][]byte{"https://x/x.png": src}}
	store := newInMemoryMediaStore()
	pipeline, err := NewProductImagePipeline(nil, ProductImagePipelineConfig{
		Downloader: dl,
		Remover:    NewStubBackgroundRemover(),
		Store:      store,
		TenantID:   "cylrl",
		KeyPrefix:  "/v320/",
	})
	if err != nil {
		t.Fatalf("NewProductImagePipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })

	res, err := pipeline.Process(context.Background(), ProductImageRequest{
		ProductID: "x",
		ImageURL:  "https://x/x.png",
		Variant:   "thumb",
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if want := "tenants/cylrl/v320/products/x/thumb.png"; res.StoredObject.Key != want {
		t.Fatalf("StoredObject.Key = %q, want %q", res.StoredObject.Key, want)
	}
}

func TestProductImage_DownloaderReturnsNotFoundString(t *testing.T) {
	t.Parallel()

	dl := &fakeDownloader{}
	store := newInMemoryMediaStore()
	pipeline, err := NewProductImagePipeline(nil, ProductImagePipelineConfig{
		Downloader: dl,
		Remover:    NewStubBackgroundRemover(),
		Store:      store,
		TenantID:   "cylrl",
	})
	if err != nil {
		t.Fatalf("NewProductImagePipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })

	_, err = pipeline.Process(context.Background(), ProductImageRequest{
		ProductID: "x",
		ImageURL:  "https://missing/no.png",
		Action:    ActionBackgroundRemoval,
	})
	if err == nil || !errors.Is(err, ErrImageProcessingFailed) {
		t.Fatalf("error = %v, want wrapping ErrImageProcessingFailed", err)
	}
	if !strings.Contains(err.Error(), "download") {
		t.Fatalf("error message = %q, want download in it", err.Error())
	}
}

func TestCopyAllReturnsBytes(t *testing.T) {
	t.Parallel()

	got, err := CopyAll(strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("CopyAll: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("CopyAll = %q, want hello", got)
	}
}

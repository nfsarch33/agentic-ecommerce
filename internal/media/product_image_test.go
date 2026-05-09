package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// TestProductImage_BackgroundRemovalProducesTransparentPNG is the
// v3.2.0 EC-2-2 RED test. It verifies that the ProductImagePipeline:
//
//  1. Downloads a supplier image via the configured Downloader.
//  2. Routes the bytes through the configured BackgroundRemover
//     (the cmd/* binary wires either a Bedrock-Vision adapter or
//     the v3.2.0 image-bridge stub; this test uses the stub
//     implementation provided by the package via NewStubBackgroundRemover
//     which removes the dominant background colour deterministically).
//  3. Stores the resulting PNG into the supplied port.MediaStore.
//  4. Returns a ProcessedImage whose Bytes encode a PNG with at
//     least one fully transparent pixel.
//  5. Records the action via the optional metrics hook.
func TestProductImage_BackgroundRemovalProducesTransparentPNG(t *testing.T) {
	t.Parallel()

	// Build a tiny synthetic image: 4x4 with a solid blue border
	// (background) and a 2x2 red square in the centre (subject).
	src := newTestPNG(t, 4, 4, color.RGBA{R: 30, G: 60, B: 200, A: 255}, color.RGBA{R: 220, G: 30, B: 30, A: 255})

	dl := &fakeDownloader{
		responses: map[string][]byte{
			"https://supplier.example.com/img/earbuds-001.png": src,
		},
	}
	store := newInMemoryMediaStore()
	pipeline, err := NewProductImagePipeline(nil, ProductImagePipelineConfig{
		Downloader: dl,
		Remover:    NewStubBackgroundRemover(),
		Store:      store,
		TenantID:   "cylrl",
		Now:        func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewProductImagePipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })

	res, err := pipeline.Process(context.Background(), ProductImageRequest{
		ProductID: "earbuds-001",
		ImageURL:  "https://supplier.example.com/img/earbuds-001.png",
		Action:    ActionBackgroundRemoval,
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if res.Action != ActionBackgroundRemoval {
		t.Fatalf("Action = %q, want %q", res.Action, ActionBackgroundRemoval)
	}
	if res.StoredObject.Key == "" || res.StoredObject.URL == "" {
		t.Fatalf("StoredObject not set: %#v", res.StoredObject)
	}
	if res.OutputContentType != "image/png" {
		t.Fatalf("OutputContentType = %q, want image/png", res.OutputContentType)
	}
	if !hasTransparentPixel(t, res.OutputBytes) {
		t.Fatalf("background removal produced PNG with zero transparent pixels")
	}
}

func TestProductImage_RejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	dl := &fakeDownloader{}
	store := newInMemoryMediaStore()

	cases := []struct {
		name string
		mut  func(c *ProductImagePipelineConfig)
	}{
		{name: "no downloader", mut: func(c *ProductImagePipelineConfig) { c.Downloader = nil }},
		{name: "no remover", mut: func(c *ProductImagePipelineConfig) { c.Remover = nil }},
		{name: "no store", mut: func(c *ProductImagePipelineConfig) { c.Store = nil }},
		{name: "no tenant", mut: func(c *ProductImagePipelineConfig) { c.TenantID = " " }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := ProductImagePipelineConfig{
				Downloader: dl,
				Remover:    NewStubBackgroundRemover(),
				Store:      store,
				TenantID:   "cylrl",
			}
			tc.mut(&cfg)
			_, err := NewProductImagePipeline(nil, cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrImagePipelineUnconfigured) {
				t.Fatalf("error not wrapping ErrImagePipelineUnconfigured: %v", err)
			}
		})
	}
}

func TestProductImage_DownloadFailureWrapsErrImageProcessingFailed(t *testing.T) {
	t.Parallel()

	dl := &fakeDownloader{
		errors: map[string]error{
			"https://x/no.png": errors.New("404"),
		},
	}
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
		ImageURL:  "https://x/no.png",
		Action:    ActionBackgroundRemoval,
	})
	if !errors.Is(err, ErrImageProcessingFailed) {
		t.Fatalf("error = %v, want ErrImageProcessingFailed", err)
	}
}

func TestProductImage_ProcessAfterCloseReturnsClosedError(t *testing.T) {
	t.Parallel()

	pipeline, err := NewProductImagePipeline(nil, ProductImagePipelineConfig{
		Downloader: &fakeDownloader{},
		Remover:    NewStubBackgroundRemover(),
		Store:      newInMemoryMediaStore(),
		TenantID:   "cylrl",
	})
	if err != nil {
		t.Fatalf("NewProductImagePipeline: %v", err)
	}
	if err := pipeline.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err = pipeline.Process(context.Background(), ProductImageRequest{ProductID: "x", ImageURL: "https://x/x.png"})
	if !errors.Is(err, ErrImagePipelineClosed) {
		t.Fatalf("error = %v, want ErrImagePipelineClosed", err)
	}
}

func TestProductImage_LifestyleGenerationIsStubbedAndDocumentedDeferred(t *testing.T) {
	t.Parallel()

	pipeline, err := NewProductImagePipeline(nil, ProductImagePipelineConfig{
		Downloader: &fakeDownloader{
			responses: map[string][]byte{
				"https://x/x.png": newTestPNG(t, 2, 2, color.RGBA{255, 255, 255, 255}, color.RGBA{0, 0, 0, 255}),
			},
		},
		Remover:  NewStubBackgroundRemover(),
		Store:    newInMemoryMediaStore(),
		TenantID: "cylrl",
	})
	if err != nil {
		t.Fatalf("NewProductImagePipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })

	_, err = pipeline.Process(context.Background(), ProductImageRequest{
		ProductID: "x",
		ImageURL:  "https://x/x.png",
		Action:    ActionLifestyleGeneration,
	})
	if !errors.Is(err, ErrImageBridgeUnconfigured) {
		t.Fatalf("error = %v, want ErrImageBridgeUnconfigured (lifestyle generation should be deferred to image-bridge story)", err)
	}
}

// --- helpers ---------------------------------------------------------------

func newTestPNG(tb testing.TB, w, h int, bg, fg color.RGBA) []byte {
	tb.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := bg
			if x >= 1 && x < w-1 && y >= 1 && y < h-1 {
				c = fg
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		tb.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

func hasTransparentPixel(t *testing.T, data []byte) bool {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode result png: %v", err)
	}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a == 0 {
				return true
			}
		}
	}
	return false
}

// fakeDownloader is the in-test ImageDownloader: maps URL -> bytes
// or URL -> error.
type fakeDownloader struct {
	responses map[string][]byte
	errors    map[string]error
}

func (f *fakeDownloader) Download(_ context.Context, url string) ([]byte, string, error) {
	if err, ok := f.errors[url]; ok {
		return nil, "", err
	}
	body, ok := f.responses[url]
	if !ok {
		return nil, "", errors.New("not found")
	}
	return body, "image/png", nil
}

// inMemoryMediaStore is a port.MediaStore double used by tests.
type inMemoryMediaStore struct {
	objects map[string][]byte
}

func newInMemoryMediaStore() *inMemoryMediaStore {
	return &inMemoryMediaStore{objects: map[string][]byte{}}
}

func (s *inMemoryMediaStore) Put(_ context.Context, object port.MediaObject) (port.StoredMediaObject, error) {
	body, err := io.ReadAll(object.Body)
	if err != nil {
		return port.StoredMediaObject{}, err
	}
	s.objects[object.Key] = body
	return port.StoredMediaObject{
		Key:         object.Key,
		URL:         "memory://" + object.Key,
		ContentType: object.ContentType,
		SizeBytes:   int64(len(body)),
		StoredAt:    time.Now().UTC(),
	}, nil
}

func (s *inMemoryMediaStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := s.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (s *inMemoryMediaStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

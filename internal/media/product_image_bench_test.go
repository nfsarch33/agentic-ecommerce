package media

import (
	"context"
	"image/color"
	"testing"
)

// BenchmarkProductImage_BackgroundRemoval measures the steady-state
// cost of the v3.2.0 EC-2-2 background removal path with the
// deterministic stub remover. Used by the regression bench so the
// next sprint can compare the bridge-backed remover against the
// stub baseline.
func BenchmarkProductImage_BackgroundRemoval(b *testing.B) {
	src := newBenchPNG(b, 32, 32, color.RGBA{R: 30, G: 60, B: 200, A: 255}, color.RGBA{R: 220, G: 30, B: 30, A: 255})
	dl := &fakeDownloader{
		responses: map[string][]byte{
			"https://supplier.example.com/img/x.png": src,
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
		b.Fatalf("NewProductImagePipeline: %v", err)
	}
	b.Cleanup(func() { _ = pipeline.Close(context.Background()) })

	req := ProductImageRequest{
		ProductID: "x",
		ImageURL:  "https://supplier.example.com/img/x.png",
		Action:    ActionBackgroundRemoval,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := pipeline.Process(context.Background(), req); err != nil {
			b.Fatalf("Process: %v", err)
		}
	}
}

func newBenchPNG(b *testing.B, w, h int, bg, fg color.RGBA) []byte {
	b.Helper()
	return newTestPNG(b, w, h, bg, fg)
}

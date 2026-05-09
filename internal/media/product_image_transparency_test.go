// File scope: v3.2.1 QA Task 3 -- StubBackgroundRemover transparency
// assertion across the standard supplier fixture set.
//
// The v3.2.0 EC-2-2 RED test (TestProductImage_BackgroundRemovalProducesTransparentPNG)
// proves the pipeline emits *at least one* transparent pixel for a
// single 4x4 fixture. The v3.2.1 acceptance criterion in the parent
// plan goes further and asks for pixel-level dominant-corner-colour
// replacement across a representative supplier image set:
//
//   - Every corner pixel (the dominant-colour ring) MUST become
//     fully transparent (alpha == 0).
//   - The centre subject pixels MUST stay opaque (alpha > 0). A
//     remover that "succeeds" by zeroing the entire image would
//     pass the v3.2.0 has-transparent-pixel check while breaking
//     the operator's expectation that the foreground product
//     survives.
//
// The fixture set covers five supplier-realistic backgrounds:
// white studio, light grey marketplace, blue accent (1688), black
// premium, and warm beige (lifestyle). Each fixture is assembled
// in-process so the test is hermetic + deterministic.
//
// Cite skill: go-clean-architecture (the table-driven cases keep
// the fixture authorship next to the assertion they exercise).
package media

import (
	"bytes"
	"context"
	"image/color"
	"image/png"
	"testing"
)

// TestStubBackgroundRemover_TransparentPNGAcrossSupplierFixtures is
// the v3.2.1 QA-3 pixel-level acceptance test. It exercises the
// StubBackgroundRemover against five supplier-realistic background
// colours and asserts the alpha channel for both corner-ring and
// centre-subject pixels.
func TestStubBackgroundRemover_TransparentPNGAcrossSupplierFixtures(t *testing.T) {
	t.Parallel()

	subject := color.RGBA{R: 220, G: 30, B: 30, A: 255} // bright red product centre

	cases := []struct {
		name string
		bg   color.RGBA
	}{
		{name: "studio_white", bg: color.RGBA{R: 250, G: 250, B: 250, A: 255}},
		{name: "marketplace_grey", bg: color.RGBA{R: 200, G: 200, B: 205, A: 255}},
		{name: "1688_blue", bg: color.RGBA{R: 30, G: 60, B: 200, A: 255}},
		{name: "premium_black", bg: color.RGBA{R: 12, G: 12, B: 12, A: 255}},
		{name: "lifestyle_beige", bg: color.RGBA{R: 232, G: 215, B: 188, A: 255}},
	}

	r := NewStubBackgroundRemover()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := newTestPNG(t, 6, 6, tc.bg, subject)
			out, err := r.Remove(context.Background(), src, "image/png")
			if err != nil {
				t.Fatalf("Remove(%s): %v", tc.name, err)
			}
			if !bytes.HasPrefix(out, []byte{0x89, 'P', 'N', 'G'}) {
				t.Fatalf("output is not PNG-encoded: first 8 bytes = %v", out[:8])
			}
			img, err := png.Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("decode result png(%s): %v", tc.name, err)
			}

			// Pixel-level assertion 1: every corner pixel MUST be
			// fully transparent. The newTestPNG helper paints a
			// 1-pixel border of `bg` so the four corners are part
			// of the dominant background ring.
			cornerPixels := [][2]int{{0, 0}, {5, 0}, {0, 5}, {5, 5}}
			for _, p := range cornerPixels {
				_, _, _, a := img.At(p[0], p[1]).RGBA()
				if a != 0 {
					t.Errorf("%s: corner (%d,%d) alpha = %d, want 0 (dominant background should be transparent)", tc.name, p[0], p[1], a)
				}
			}

			// Pixel-level assertion 2: at least the centre subject
			// pixels (the inner 4x4 block) MUST stay opaque. Without
			// this, a remover could "pass" the simple has-one-
			// transparent-pixel check by zeroing the whole image.
			//
			// The newTestPNG helper assembles the subject in the
			// inner ring (1..size-2). A 6x6 image therefore has a
			// 4x4 subject block at (1..4, 1..4).
			subjectOpaqueCount := 0
			for y := 1; y <= 4; y++ {
				for x := 1; x <= 4; x++ {
					_, _, _, a := img.At(x, y).RGBA()
					if a > 0 {
						subjectOpaqueCount++
					}
				}
			}
			if subjectOpaqueCount < 12 {
				t.Errorf("%s: only %d/16 subject pixels remain opaque -- remover ate too much", tc.name, subjectOpaqueCount)
			}

			// Pixel-level assertion 3: count total transparent
			// pixels and verify it is at least the perimeter (20
			// pixels = 4 corners + the rest of the 1-pixel border
			// on a 6x6 image). This guards against a remover that
			// only zeros the literal corners while ignoring the
			// rest of the dominant ring.
			perimeterTransparent := countTransparentPerimeter(img, 6, 6)
			if perimeterTransparent < 20 {
				t.Errorf("%s: only %d/20 perimeter pixels became transparent", tc.name, perimeterTransparent)
			}
		})
	}
}

// countTransparentPerimeter counts pixels with alpha == 0 along the
// outer ring of a width x height image. Helper kept tiny so the
// assertion path is unit-testable in isolation.
func countTransparentPerimeter(img interface{ At(x, y int) color.Color }, w, h int) int {
	count := 0
	for x := 0; x < w; x++ {
		if _, _, _, a := img.At(x, 0).RGBA(); a == 0 {
			count++
		}
		if _, _, _, a := img.At(x, h-1).RGBA(); a == 0 {
			count++
		}
	}
	for y := 1; y < h-1; y++ {
		if _, _, _, a := img.At(0, y).RGBA(); a == 0 {
			count++
		}
		if _, _, _, a := img.At(w-1, y).RGBA(); a == 0 {
			count++
		}
	}
	return count
}

// TestStubBackgroundRemover_PreservesAlreadyTransparentPixels
// covers a corner case the v3.2.0 path documents: an input image
// with pre-existing alpha=0 pixels (e.g. a partially-cut supplier
// PNG) must keep those transparent through the remover.
func TestStubBackgroundRemover_PreservesAlreadyTransparentPixels(t *testing.T) {
	t.Parallel()

	// Build a 4x4 image where the top-left pixel is already fully
	// transparent (alpha=0) and the rest is solid blue.
	src := newTestPNG(t, 4, 4, color.RGBA{R: 30, G: 60, B: 200, A: 255}, color.RGBA{R: 220, G: 30, B: 30, A: 255})
	// Decode + edit + re-encode to introduce the transparent pixel.
	srcImg, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("decode helper png: %v", err)
	}
	bounds := srcImg.Bounds()
	editable, ok := srcImg.(interface {
		Set(x, y int, c color.Color)
	})
	if !ok {
		// helper PNG is NRGBA so this should always succeed; defensively skip.
		t.Skipf("helper png type does not support Set: %T", srcImg)
	}
	editable.Set(bounds.Min.X, bounds.Min.Y, color.NRGBA{0, 0, 0, 0})
	var buf bytes.Buffer
	if err := png.Encode(&buf, srcImg); err != nil {
		t.Fatalf("re-encode src: %v", err)
	}

	r := NewStubBackgroundRemover()
	out, err := r.Remove(context.Background(), buf.Bytes(), "image/png")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	_, _, _, a := got.At(0, 0).RGBA()
	if a != 0 {
		t.Fatalf("pre-existing transparent pixel was lost: alpha = %d", a)
	}
}

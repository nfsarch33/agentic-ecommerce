package media

import "testing"

func TestProcessorAcceptsAllowedImageWithUsefulAltText(t *testing.T) {
	t.Parallel()

	processor := NewProcessor(DefaultConstraints())
	got := processor.Validate(ImageMetadata{
		URL:         "https://cdn.example.com/products/band.jpg",
		MimeType:    "image/jpeg",
		SizeBytes:   512_000,
		Width:       1200,
		Height:      900,
		AltText:     "Resistance band set with handles",
		ProductName: "Resistance Band Set",
	})

	if !got.Pass {
		t.Fatalf("result = %#v, want pass", got)
	}
	if got.Score != 100 {
		t.Fatalf("score = %d, want 100", got.Score)
	}
}

func TestProcessorRejectsUnsupportedMimeOversizeAndMissingAlt(t *testing.T) {
	t.Parallel()

	processor := NewProcessor(DefaultConstraints())
	got := processor.Validate(ImageMetadata{
		URL:       "https://cdn.example.com/products/band.bmp",
		MimeType:  "image/bmp",
		SizeBytes: 6 * 1024 * 1024,
		Width:     320,
		Height:    200,
		AltText:   "",
	})

	if got.Pass {
		t.Fatal("expected image validation to fail")
	}
	for _, want := range []string{"unsupported_mime_type", "image_too_large", "alt_text_required", "image_dimensions_too_small"} {
		if !hasMediaReason(got.Reasons, want) {
			t.Fatalf("reasons = %#v, missing %q", got.Reasons, want)
		}
	}
}

func TestValidateAltTextRejectsKeywordStuffingAndGenericCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		alt  string
	}{
		{name: "generic", alt: "product image"},
		{name: "stuffed", alt: "resistance resistance resistance resistance band set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ValidateAltText(tt.alt, "Resistance Band Set")
			if got.Pass {
				t.Fatalf("ValidateAltText(%q) passed, want fail", tt.alt)
			}
		})
	}
}

func TestProcessorAppliesDefaultConstraintsAndInfersMimeFromURL(t *testing.T) {
	t.Parallel()

	processor := NewProcessor(Constraints{})
	got := processor.Validate(ImageMetadata{
		URL:       "https://cdn.example.com/products/band.webp?width=1200",
		SizeBytes: 1024,
		Width:     800,
		Height:    800,
		AltText:   "Resistance band set packed in carry bag",
	})

	if !got.Pass {
		t.Fatalf("result = %#v, want pass", got)
	}
}

func TestValidateAltTextRejectsShortAndOverlongCopy(t *testing.T) {
	t.Parallel()

	short := ValidateAltText("band", "Resistance Band Set")
	if short.Pass || !hasMediaReason(short.Reasons, "alt_text_too_short") {
		t.Fatalf("short alt result = %#v", short)
	}

	longText := "Resistance band set with handles, door anchor, carry bag, workout guide, extra loops, compact storage pouch, and detailed setup accessories shown on a clean studio background"
	long := ValidateAltText(longText, "Resistance Band Set")
	if long.Pass || !hasMediaReason(long.Reasons, "alt_text_too_long") {
		t.Fatalf("long alt result = %#v", long)
	}
}

func BenchmarkMediaValidation(b *testing.B) {
	processor := NewProcessor(DefaultConstraints())
	image := ImageMetadata{
		URL:         "https://cdn.example.com/products/band.webp?width=1200",
		MimeType:    "image/webp; charset=binary",
		SizeBytes:   512_000,
		Width:       1200,
		Height:      900,
		AltText:     "Resistance band set with handles, anchor, and carry bag",
		ProductName: "Resistance Band Set",
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if result := processor.Validate(image); !result.Pass {
			b.Fatalf("validation failed: %#v", result)
		}
	}
}

func hasMediaReason(reasons []Reason, id string) bool {
	for _, reason := range reasons {
		if reason.ID == id {
			return true
		}
	}
	return false
}

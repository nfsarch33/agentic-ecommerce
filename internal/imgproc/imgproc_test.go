package imgproc_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/imgproc"
)

// --- AspectFit ---

func TestAspectFit_LandscapeIntoSquare(t *testing.T) {
	t.Parallel()
	orig := imgproc.Dimensions{Width: 1600, Height: 900}
	got, err := imgproc.AspectFit(orig, 800, 800)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 800 {
		t.Fatalf("want width 800, got %d", got.Width)
	}
	if got.Height != 450 {
		t.Fatalf("want height 450, got %d", got.Height)
	}
}

func TestAspectFit_PortraitConstrainedByHeight(t *testing.T) {
	t.Parallel()
	orig := imgproc.Dimensions{Width: 400, Height: 800}
	got, err := imgproc.AspectFit(orig, 400, 400)
	if err != nil {
		t.Fatal(err)
	}
	if got.Height != 400 {
		t.Fatalf("want height 400, got %d", got.Height)
	}
	if got.Width != 200 {
		t.Fatalf("want width 200, got %d", got.Width)
	}
}

func TestAspectFit_InvalidDimensions(t *testing.T) {
	t.Parallel()
	_, err := imgproc.AspectFit(imgproc.Dimensions{Width: 0, Height: 100}, 200, 200)
	if !errors.Is(err, imgproc.ErrInvalidDimensions) {
		t.Fatalf("want ErrInvalidDimensions, got %v", err)
	}
}

// --- AspectFill ---

func TestAspectFill_CoversTarget(t *testing.T) {
	t.Parallel()
	orig := imgproc.Dimensions{Width: 1600, Height: 900}
	got, err := imgproc.AspectFill(orig, 800, 800)
	if err != nil {
		t.Fatal(err)
	}
	// The larger scale is chosen so both dimensions >= 800.
	if got.Width < 800 || got.Height < 800 {
		t.Fatalf("fill must cover target: got %+v", got)
	}
}

// --- Resize ---

func TestResize_FitMode(t *testing.T) {
	t.Parallel()
	orig := imgproc.Dimensions{Width: 1000, Height: 500}
	got, err := imgproc.Resize(orig, imgproc.ThumbnailSpec{Width: 200, Height: 200, Fit: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Width > 200 || got.Height > 200 {
		t.Fatalf("fit mode must not exceed target: got %+v", got)
	}
}

// --- ValidateFormat ---

func TestValidateFormat_Supported(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"photo.jpg", "img.PNG", "anim.gif", "img.webp"} {
		if err := imgproc.ValidateFormat(name); err != nil {
			t.Errorf("unexpected error for %q: %v", name, err)
		}
	}
}

func TestValidateFormat_Unsupported(t *testing.T) {
	t.Parallel()
	err := imgproc.ValidateFormat("document.pdf")
	if !errors.Is(err, imgproc.ErrUnsupportedFormat) {
		t.Fatalf("want ErrUnsupportedFormat, got %v", err)
	}
}

// --- CDNURLBuilder ---

func TestCDNURLBuilder_ResizeURL(t *testing.T) {
	t.Parallel()
	b := imgproc.NewCDNURLBuilder("https://cdn.example.com")
	url := b.ResizeURL("images/photo.jpg", 640, 480, true)
	if !strings.Contains(url, "w_640") || !strings.Contains(url, "h_480") {
		t.Fatalf("unexpected URL: %s", url)
	}
}

func TestCDNURLBuilder_ThumbnailURL(t *testing.T) {
	t.Parallel()
	b := imgproc.NewCDNURLBuilder("https://cdn.example.com")
	url := b.ThumbnailURL("img.png")
	if !strings.Contains(url, "w_300") {
		t.Fatalf("thumbnail URL should contain w_300: %s", url)
	}
}

func TestCDNURLBuilder_GenerateVariants(t *testing.T) {
	t.Parallel()
	b := imgproc.NewCDNURLBuilder("https://cdn.example.com")
	v := b.GenerateVariants("product/hero.jpg")
	if v.Original == "" || v.Thumbnail == "" || v.Small == "" || v.Medium == "" || v.Large == "" {
		t.Fatalf("all variant URLs must be non-empty: %+v", v)
	}
	if v.Original == v.Thumbnail {
		t.Fatal("original and thumbnail must differ")
	}
}

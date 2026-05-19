// Package imgproc provides image processing: resize, thumbnail, and CDN URL generation.
// It uses pure-Go dimension calculations; actual pixel manipulation is handled by a pluggable Processor.
package imgproc

import (
	"errors"
	"fmt"
	"math"
	"path"
	"strings"
)

// ErrInvalidDimensions is returned when width or height are not positive.
var ErrInvalidDimensions = errors.New("imgproc: invalid dimensions")

// ErrUnsupportedFormat is returned for unrecognised image extensions.
var ErrUnsupportedFormat = errors.New("imgproc: unsupported format")

// Dimensions holds an image's width and height in pixels.
type Dimensions struct {
	Width  int
	Height int
}

// AspectFit computes the largest dimensions that fit within maxW x maxH while preserving the aspect ratio.
func AspectFit(orig Dimensions, maxW, maxH int) (Dimensions, error) {
	if orig.Width <= 0 || orig.Height <= 0 || maxW <= 0 || maxH <= 0 {
		return Dimensions{}, ErrInvalidDimensions
	}
	scaleW := float64(maxW) / float64(orig.Width)
	scaleH := float64(maxH) / float64(orig.Height)
	scale := math.Min(scaleW, scaleH)
	return Dimensions{
		Width:  max1(int(math.Round(float64(orig.Width)*scale)), 1),
		Height: max1(int(math.Round(float64(orig.Height)*scale)), 1),
	}, nil
}

// AspectFill computes the smallest dimensions that cover maxW x maxH while preserving aspect ratio
// (cover / fill mode – the image is cropped to the target).
func AspectFill(orig Dimensions, targetW, targetH int) (Dimensions, error) {
	if orig.Width <= 0 || orig.Height <= 0 || targetW <= 0 || targetH <= 0 {
		return Dimensions{}, ErrInvalidDimensions
	}
	scaleW := float64(targetW) / float64(orig.Width)
	scaleH := float64(targetH) / float64(orig.Height)
	scale := math.Max(scaleW, scaleH)
	return Dimensions{
		Width:  max1(int(math.Round(float64(orig.Width)*scale)), 1),
		Height: max1(int(math.Round(float64(orig.Height)*scale)), 1),
	}, nil
}

func max1(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ThumbnailSpec prescribes thumbnail dimensions and the crop/fit strategy.
type ThumbnailSpec struct {
	Width   int
	Height  int
	Fit     bool // true = AspectFit, false = AspectFill (crop)
}

// Resize computes the output dimensions for a resize operation.
func Resize(orig Dimensions, spec ThumbnailSpec) (Dimensions, error) {
	if spec.Fit {
		return AspectFit(orig, spec.Width, spec.Height)
	}
	return AspectFill(orig, spec.Width, spec.Height)
}

// SupportedFormats is the set of image extensions handled.
var SupportedFormats = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true,
}

// ValidateFormat checks the filename extension.
func ValidateFormat(filename string) error {
	ext := strings.ToLower(path.Ext(filename))
	if !SupportedFormats[ext] {
		return fmt.Errorf("%w: %q", ErrUnsupportedFormat, ext)
	}
	return nil
}

// CDNURLBuilder generates CDN transform URLs (e.g., Cloudinary-style).
type CDNURLBuilder struct {
	BaseURL string
}

// NewCDNURLBuilder returns a builder with the given base URL.
func NewCDNURLBuilder(baseURL string) *CDNURLBuilder {
	return &CDNURLBuilder{BaseURL: strings.TrimRight(baseURL, "/")}
}

// ResizeURL builds a CDN URL that applies a server-side resize.
// Pattern: {base}/w_{width},h_{height},{mode}/{key}
func (b *CDNURLBuilder) ResizeURL(key string, width, height int, fit bool) string {
	mode := "c_fill"
	if fit {
		mode = "c_fit"
	}
	return fmt.Sprintf("%s/w_%d,h_%d,%s/%s", b.BaseURL, width, height, mode, key)
}

// ThumbnailURL builds a CDN URL for a standard thumbnail (always AspectFit, max 300x300).
func (b *CDNURLBuilder) ThumbnailURL(key string) string {
	return b.ResizeURL(key, 300, 300, true)
}

// OriginalURL returns the unmodified CDN URL.
func (b *CDNURLBuilder) OriginalURL(key string) string {
	return fmt.Sprintf("%s/%s", b.BaseURL, key)
}

// ImageVariants holds URLs for common image variants.
type ImageVariants struct {
	Original  string
	Thumbnail string
	Small     string
	Medium    string
	Large     string
}

// GenerateVariants returns a set of CDN URLs for standard sizes.
func (b *CDNURLBuilder) GenerateVariants(key string) ImageVariants {
	return ImageVariants{
		Original:  b.OriginalURL(key),
		Thumbnail: b.ResizeURL(key, 100, 100, true),
		Small:     b.ResizeURL(key, 320, 240, true),
		Medium:    b.ResizeURL(key, 640, 480, true),
		Large:     b.ResizeURL(key, 1280, 960, true),
	}
}

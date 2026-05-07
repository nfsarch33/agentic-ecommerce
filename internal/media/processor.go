package media

import (
	"net/http"
	"strings"
)

type ImageMetadata struct {
	URL         string `json:"url"`
	MimeType    string `json:"mime_type"`
	SizeBytes   int64  `json:"size_bytes"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	AltText     string `json:"alt_text"`
	ProductName string `json:"product_name,omitempty"`
}

type Constraints struct {
	AllowedMimeTypes map[string]struct{} `json:"allowed_mime_types"`
	MaxSizeBytes     int64               `json:"max_size_bytes"`
	MinWidth         int                 `json:"min_width"`
	MinHeight        int                 `json:"min_height"`
}

type Reason struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type Result struct {
	Pass    bool     `json:"pass"`
	Score   int      `json:"score"`
	Reasons []Reason `json:"reasons"`
}

type Processor struct {
	constraints Constraints
}

func DefaultConstraints() Constraints {
	return Constraints{
		AllowedMimeTypes: map[string]struct{}{
			"image/jpeg": {},
			"image/png":  {},
			"image/webp": {},
			"image/gif":  {},
		},
		MaxSizeBytes: 5 * 1024 * 1024,
		MinWidth:     600,
		MinHeight:    600,
	}
}

func NewProcessor(constraints Constraints) Processor {
	if len(constraints.AllowedMimeTypes) == 0 {
		constraints.AllowedMimeTypes = DefaultConstraints().AllowedMimeTypes
	}
	if constraints.MaxSizeBytes <= 0 {
		constraints.MaxSizeBytes = DefaultConstraints().MaxSizeBytes
	}
	if constraints.MinWidth <= 0 {
		constraints.MinWidth = DefaultConstraints().MinWidth
	}
	if constraints.MinHeight <= 0 {
		constraints.MinHeight = DefaultConstraints().MinHeight
	}
	return Processor{constraints: constraints}
}

func (p Processor) Validate(image ImageMetadata) Result {
	reasons := make([]Reason, 0)
	mimeType := normalizeMimeType(image.MimeType)
	if mimeType == "" && image.URL != "" {
		mimeType = mimeFromExtension(image.URL)
	}
	if _, ok := p.constraints.AllowedMimeTypes[mimeType]; !ok {
		reasons = append(reasons, Reason{ID: "unsupported_mime_type", Message: "image mime type must be jpeg, png, webp, or gif"})
	}
	if image.SizeBytes > p.constraints.MaxSizeBytes {
		reasons = append(reasons, Reason{ID: "image_too_large", Message: "image exceeds the maximum allowed size"})
	}
	if image.Width > 0 && image.Height > 0 && (image.Width < p.constraints.MinWidth || image.Height < p.constraints.MinHeight) {
		reasons = append(reasons, Reason{ID: "image_dimensions_too_small", Message: "image dimensions are below the minimum display size"})
	}
	alt := ValidateAltText(image.AltText, image.ProductName)
	reasons = append(reasons, alt.Reasons...)
	return resultFromReasons(reasons)
}

func ValidateAltText(altText, productName string) Result {
	alt := strings.ToLower(strings.TrimSpace(altText))
	reasons := make([]Reason, 0)
	switch {
	case alt == "":
		reasons = append(reasons, Reason{ID: "alt_text_required", Message: "image alt text is required"})
	case len([]rune(alt)) < 8:
		reasons = append(reasons, Reason{ID: "alt_text_too_short", Message: "image alt text is too short"})
	case isGenericAltText(alt):
		reasons = append(reasons, Reason{ID: "alt_text_generic", Message: "image alt text must describe the product image"})
	}
	if repeatedWordCount(alt) > 2 {
		reasons = append(reasons, Reason{ID: "alt_text_keyword_stuffed", Message: "image alt text repeats the same keyword too often"})
	}
	if productName != "" && len([]rune(alt)) > 140 {
		reasons = append(reasons, Reason{ID: "alt_text_too_long", Message: "image alt text should stay concise"})
	}
	return resultFromReasons(reasons)
}

func resultFromReasons(reasons []Reason) Result {
	score := 100 - len(reasons)*20
	if score < 0 {
		score = 0
	}
	return Result{Pass: len(reasons) == 0, Score: score, Reasons: reasons}
}

func normalizeMimeType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if idx := strings.Index(value, ";"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return value
}

func mimeFromExtension(url string) string {
	path := strings.ToLower(strings.TrimSpace(url))
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	switch {
	case strings.HasSuffix(path, ".jpg"), strings.HasSuffix(path, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(path, ".png"):
		return "image/png"
	case strings.HasSuffix(path, ".webp"):
		return "image/webp"
	case strings.HasSuffix(path, ".gif"):
		return "image/gif"
	default:
		if detected := http.DetectContentType([]byte(path)); strings.HasPrefix(detected, "image/") {
			return detected
		}
		return ""
	}
}

func isGenericAltText(alt string) bool {
	switch alt {
	case "image", "photo", "product", "product image", "product photo", "image of product":
		return true
	default:
		return false
	}
}

func repeatedWordCount(text string) int {
	counts := map[string]int{}
	maxCount := 0
	for _, word := range strings.Fields(text) {
		word = strings.Trim(word, ".,;:!?()[]{}\"'")
		if word == "" {
			continue
		}
		counts[word]++
		if counts[word] > maxCount {
			maxCount = counts[word]
		}
	}
	return maxCount
}

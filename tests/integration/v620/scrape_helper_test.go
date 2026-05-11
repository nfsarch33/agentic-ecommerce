package v620

import (
	"bytes"
	"net/http"
	"net/http/httptest"
)

// scrapeRecorder is a minimal http.ResponseWriter capturing the
// metrics body without pulling httptest into the public surface.
type scrapeRecorder struct {
	*httptest.ResponseRecorder
	body *bytes.Buffer
}

func newScrapeRecorder() *scrapeRecorder {
	rec := httptest.NewRecorder()
	return &scrapeRecorder{ResponseRecorder: rec, body: rec.Body}
}

func newScrapeRequest() *http.Request { return httptest.NewRequest(http.MethodGet, "/metrics", nil) }

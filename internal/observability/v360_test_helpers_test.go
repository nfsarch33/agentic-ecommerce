package observability

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recordingResponseWriter is the minimal http.ResponseWriter used
// by the v360 metrics test to capture the /metrics body.
type recordingResponseWriter struct {
	header http.Header
	body   *bytes.Buffer
	status int
}

func newRecordingResponseWriter() *recordingResponseWriter {
	return &recordingResponseWriter{header: http.Header{}, body: new(bytes.Buffer)}
}

func (w *recordingResponseWriter) Header() http.Header         { return w.header }
func (w *recordingResponseWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *recordingResponseWriter) WriteHeader(s int)           { w.status = s }

func mustGetReq(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, "/metrics", nil)
}

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/media/intelligence"
)

func TestMediaSourceProcessValidateAndGetEndpoints(t *testing.T) {
	t.Parallel()

	spec := loadOpenAPIContract(t)
	srv, _ := testServer(t)
	srv.mediaService = intelligence.NewService(intelligence.ServiceConfig{HTTPClient: mediaRoundTripClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(strings.NewReader(mediaOnePixelPNGString())),
		}, nil
	})})

	sourceReq := bytes.NewBufferString(`{"url":"https://supplier.example/images/lamp.png","product_id":"product-123","alt_text":"Matte black desk lamp on white background"}`)
	sourceRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(sourceRec, httptest.NewRequest(http.MethodPost, "/api/v1/media/source", sourceReq))
	if sourceRec.Code != http.StatusCreated {
		t.Fatalf("source status = %d, body=%s", sourceRec.Code, sourceRec.Body.String())
	}
	sourceBody := append([]byte(nil), sourceRec.Body.Bytes()...)
	var sourced intelligence.Asset
	if err := json.NewDecoder(bytes.NewReader(sourceBody)).Decode(&sourced); err != nil {
		t.Fatalf("decode source: %v", err)
	}
	assertMediaPayloadMatchesContract(t, spec, sourceBody, "/api/v1/media/source", http.MethodPost, "201")
	if sourced.ID == "" || sourced.Metadata.ChecksumSHA256 == "" || sourced.Metadata.Width != 1 {
		t.Fatalf("sourced = %+v", sourced)
	}

	processReq := bytes.NewBufferString(`{"media_id":"` + sourced.ID + `","format":"image/webp","resize":{"max_width":600,"max_height":600},"remove_background":true}`)
	processRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(processRec, httptest.NewRequest(http.MethodPost, "/api/v1/media/process", processReq))
	if processRec.Code != http.StatusOK {
		t.Fatalf("process status = %d, body=%s", processRec.Code, processRec.Body.String())
	}
	processBody := append([]byte(nil), processRec.Body.Bytes()...)
	var processed intelligence.Asset
	if err := json.NewDecoder(bytes.NewReader(processBody)).Decode(&processed); err != nil {
		t.Fatalf("decode process: %v", err)
	}
	assertMediaPayloadMatchesContract(t, spec, processBody, "/api/v1/media/process", http.MethodPost, "200")
	if processed.Metadata.MimeType != "image/webp" {
		t.Fatalf("processed = %+v", processed)
	}

	validateRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(validateRec, httptest.NewRequest(http.MethodPost, "/api/v1/media/"+processed.ID+"/validate", nil))
	if validateRec.Code != http.StatusOK {
		t.Fatalf("validate status = %d, body=%s", validateRec.Code, validateRec.Body.String())
	}
	validateBody := append([]byte(nil), validateRec.Body.Bytes()...)
	var qa intelligence.QualityReport
	if err := json.NewDecoder(bytes.NewReader(validateBody)).Decode(&qa); err != nil {
		t.Fatalf("decode qa: %v", err)
	}
	assertMediaPayloadMatchesContract(t, spec, validateBody, "/api/v1/media/{id}/validate", http.MethodPost, "200")
	if qa.Score == 0 || len(qa.Issues) == 0 {
		t.Fatalf("qa = %+v, want explicit quality issues for 1x1 fixture", qa)
	}

	getRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/api/v1/media/"+processed.ID, nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body=%s", getRec.Code, getRec.Body.String())
	}
	assertMediaPayloadMatchesContract(t, spec, getRec.Body.Bytes(), "/api/v1/media/{id}", http.MethodGet, "200")
}

func TestMediaEndpointsValidateRequestBodies(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	srv.mediaService = intelligence.NewService(intelligence.ServiceConfig{})

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "source invalid json", method: http.MethodPost, path: "/api/v1/media/source", body: `{`, want: http.StatusBadRequest},
		{name: "source missing url", method: http.MethodPost, path: "/api/v1/media/source", body: `{}`, want: http.StatusUnprocessableEntity},
		{name: "process missing media id", method: http.MethodPost, path: "/api/v1/media/process", body: `{}`, want: http.StatusUnprocessableEntity},
		{name: "get missing media", method: http.MethodGet, path: "/api/v1/media/missing", body: ``, want: http.StatusNotFound},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			srv.mux().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestWriteMediaErrorMapsKnownFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid source", err: intelligence.ErrInvalidSourceURL, want: http.StatusUnprocessableEntity},
		{name: "missing client", err: intelligence.ErrHTTPClientRequired, want: http.StatusServiceUnavailable},
		{name: "missing media", err: intelligence.ErrMediaNotFound, want: http.StatusNotFound},
		{name: "source failed", err: intelligence.ErrSourceFailed, want: http.StatusBadGateway},
		{name: "missing store", err: intelligence.ErrStoreRequired, want: http.StatusServiceUnavailable},
		{name: "unknown", err: errors.New("boom"), want: http.StatusInternalServerError},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			writeMediaError(rec, tc.err)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestMediaHandlersReportUnconfiguredService(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "source", method: http.MethodPost, path: "/api/v1/media/source", body: `{"url":"https://supplier.example/image.png"}`},
		{name: "process", method: http.MethodPost, path: "/api/v1/media/process", body: `{"media_id":"media-1"}`},
		{name: "validate", method: http.MethodPost, path: "/api/v1/media/media-1/validate"},
		{name: "get", method: http.MethodGet, path: "/api/v1/media/media-1"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			srv.mux().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

type mediaRoundTripClient func(*http.Request) (*http.Response, error)

func (c mediaRoundTripClient) Do(req *http.Request) (*http.Response, error) {
	return c(req)
}

func mediaOnePixelPNGString() string {
	raw, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	return string(raw)
}

func assertMediaPayloadMatchesContract(t *testing.T, spec openAPIContract, body []byte, path, method, status string) {
	t.Helper()
	var payload map[string]any
	decodeJSONPayload(t, body, &payload)
	schema := responseSchema(t, spec, path, method, status)
	assertSchemaRequiredFields(t, spec, schema, payload)
}

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
	sourced := mustSourceMediaAsset(t, srv, spec, sourceReq)
	mustApproveMediaAsset(t, srv, spec, sourced.ID, `{"reviewer":"lead@example.com","note":"ready for processing"}`)

	processReq := bytes.NewBufferString(`{"media_id":"` + sourced.ID + `","format":"image/webp","resize":{"max_width":600,"max_height":600},"remove_background":true}`)
	processed := mustProcessMediaAsset(t, srv, spec, processReq)
	mustValidateMediaAsset(t, srv, spec, processed.ID)
	mustGetMediaAsset(t, srv, spec, processed.ID)
}

func TestMediaSourceEndpointMarksDuplicateRequestsAsReplay(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	srv.mediaService = intelligence.NewService(intelligence.ServiceConfig{HTTPClient: mediaRoundTripClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(strings.NewReader(mediaOnePixelPNGString())),
		}, nil
	})})

	body := `{"url":"https://supplier.example/images/lamp.png","product_id":"product-123","alt_text":"Matte black desk lamp on white background"}`
	firstRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(firstRec, httptest.NewRequest(http.MethodPost, "/api/v1/media/source", strings.NewReader(body)))
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("first source status = %d, want 201; body=%s", firstRec.Code, firstRec.Body.String())
	}

	secondRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(secondRec, httptest.NewRequest(http.MethodPost, "/api/v1/media/source", strings.NewReader(body)))
	if secondRec.Code != http.StatusOK {
		t.Fatalf("duplicate source status = %d, want 200; body=%s", secondRec.Code, secondRec.Body.String())
	}
}

func TestMediaReviewEndpointsGateProcessingLifecycle(t *testing.T) {
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

	sourced := mustSourceMediaAsset(t, srv, spec, strings.NewReader(`{"url":"https://supplier.example/images/lamp.png","product_id":"product-123","alt_text":"Matte black desk lamp on white background"}`))

	assertMediaConflict(t, srv, http.MethodPost, "/api/v1/media/process", strings.NewReader(`{"media_id":"`+sourced.ID+`"}`), "pending process")
	mustApproveMediaAsset(t, srv, spec, sourced.ID, `{"reviewer":"lead@example.com","note":"ready for processing"}`)
	mustProcessMediaAsset(t, srv, spec, bytes.NewBufferString(`{"media_id":"`+sourced.ID+`","format":"image/webp"}`))
	assertMediaConflict(t, srv, http.MethodPost, "/api/v1/media/"+sourced.ID+"/reject", strings.NewReader(`{"reviewer":"qa@example.com","note":"brand mismatch"}`), "reject after approval")
}

func TestMediaRejectEndpointMakesAssetUnprocessable(t *testing.T) {
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

	sourced := mustSourceMediaAsset(t, srv, spec, strings.NewReader(`{"url":"https://supplier.example/images/lamp.png","product_id":"product-456","alt_text":"Matte black desk lamp on white background"}`))

	rejectRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rejectRec, httptest.NewRequest(http.MethodPost, "/api/v1/media/"+sourced.ID+"/reject", strings.NewReader(`{"reviewer":"qa@example.com","note":"brand mismatch"}`)))
	if rejectRec.Code != http.StatusOK {
		t.Fatalf("reject status = %d, want 200; body=%s", rejectRec.Code, rejectRec.Body.String())
	}
	assertMediaPayloadMatchesContract(t, spec, rejectRec.Body.Bytes(), "/api/v1/media/{id}/reject", http.MethodPost, "200")

	processRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(processRec, httptest.NewRequest(http.MethodPost, "/api/v1/media/process", strings.NewReader(`{"media_id":"`+sourced.ID+`"}`)))
	if processRec.Code != http.StatusConflict {
		t.Fatalf("rejected process status = %d, want 409; body=%s", processRec.Code, processRec.Body.String())
	}
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
		{name: "approve missing note body", method: http.MethodPost, path: "/api/v1/media/media-1/reject", body: `{}`, want: http.StatusUnprocessableEntity},
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
		{name: "state conflict", err: errors.New("media lifecycle conflict"), want: http.StatusConflict},
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

func mustSourceMediaAsset(t *testing.T, srv *server, spec openAPIContract, body io.Reader) intelligence.Asset {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/media/source", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("source status = %d, body=%s", rec.Code, rec.Body.String())
	}
	raw := append([]byte(nil), rec.Body.Bytes()...)
	asset := decodeMediaAsset(t, raw, "source")
	assertMediaPayloadMatchesContract(t, spec, raw, "/api/v1/media/source", http.MethodPost, "201")
	assertSourcedMediaAsset(t, asset)
	return asset
}

func mustApproveMediaAsset(t *testing.T, srv *server, spec openAPIContract, mediaID, body string) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/media/"+mediaID+"/approve", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body=%s", rec.Code, rec.Body.String())
	}
	assertMediaPayloadMatchesContract(t, spec, rec.Body.Bytes(), "/api/v1/media/{id}/approve", http.MethodPost, "200")
}

func mustProcessMediaAsset(t *testing.T, srv *server, spec openAPIContract, body *bytes.Buffer) intelligence.Asset {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/media/process", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("process status = %d, body=%s", rec.Code, rec.Body.String())
	}
	raw := append([]byte(nil), rec.Body.Bytes()...)
	asset := decodeMediaAsset(t, raw, "process")
	assertMediaPayloadMatchesContract(t, spec, raw, "/api/v1/media/process", http.MethodPost, "200")
	if asset.Metadata.MimeType != "image/webp" {
		t.Fatalf("processed = %+v", asset)
	}
	return asset
}

func assertMediaConflict(t *testing.T, srv *server, method, path string, body io.Reader, label string) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(method, path, body))
	if rec.Code != http.StatusConflict {
		t.Fatalf("%s status = %d, want 409; body=%s", label, rec.Code, rec.Body.String())
	}
}

func decodeMediaAsset(t *testing.T, raw []byte, label string) intelligence.Asset {
	t.Helper()
	var asset intelligence.Asset
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&asset); err != nil {
		t.Fatalf("decode %s: %v", label, err)
	}
	return asset
}

func assertSourcedMediaAsset(t *testing.T, asset intelligence.Asset) {
	t.Helper()
	if asset.ID == "" || asset.Metadata.ChecksumSHA256 == "" || asset.Metadata.Width != 1 {
		t.Fatalf("sourced = %+v", asset)
	}
}

func mustValidateMediaAsset(t *testing.T, srv *server, spec openAPIContract, mediaID string) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/media/"+mediaID+"/validate", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("validate status = %d, body=%s", rec.Code, rec.Body.String())
	}
	raw := append([]byte(nil), rec.Body.Bytes()...)
	var qa intelligence.QualityReport
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&qa); err != nil {
		t.Fatalf("decode qa: %v", err)
	}
	assertMediaPayloadMatchesContract(t, spec, raw, "/api/v1/media/{id}/validate", http.MethodPost, "200")
	if qa.Score == 0 || len(qa.Issues) == 0 {
		t.Fatalf("qa = %+v, want explicit quality issues for 1x1 fixture", qa)
	}
}

func mustGetMediaAsset(t *testing.T, srv *server, spec openAPIContract, mediaID string) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/media/"+mediaID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body=%s", rec.Code, rec.Body.String())
	}
	assertMediaPayloadMatchesContract(t, spec, rec.Body.Bytes(), "/api/v1/media/{id}", http.MethodGet, "200")
}

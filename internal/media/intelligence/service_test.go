package intelligence

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/source_metadata.golden.json testdata/quality_report.golden.json
var goldenMetadata embed.FS

func TestServiceSourcesImageMetadataFromDeterministicHTTPClient(t *testing.T) {
	t.Parallel()

	client := roundTripClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://supplier.example/images/lamp.png" {
			t.Fatalf("url = %q", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}, "Content-Length": []string{"68"}},
			Body:       io.NopCloser(strings.NewReader(onePixelPNGString())),
		}, nil
	})
	service := NewService(ServiceConfig{HTTPClient: client})

	got, err := service.Source(context.Background(), SourceRequest{
		URL:       "https://supplier.example/images/lamp.png",
		ProductID: "product-123",
		AltText:   "Matte black desk lamp on white background",
	})
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	if got.ID == "" || got.ProductID != "product-123" || got.SourceURL != "https://supplier.example/images/lamp.png" {
		t.Fatalf("asset identity = %+v", got)
	}
	assertGoldenMetadata(t, got.Metadata)
	if got.AltText != "Matte black desk lamp on white background" {
		t.Fatalf("alt text = %q", got.AltText)
	}
}

func TestServiceProcessesImageWithDeterministicStub(t *testing.T) {
	t.Parallel()

	service := NewService(ServiceConfig{HTTPClient: fixedImageClient(t)})
	source, err := service.Source(context.Background(), SourceRequest{
		URL:       "https://supplier.example/images/lamp.png",
		ProductID: "product-123",
		AltText:   "Matte black desk lamp on white background",
	})
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	mustApproveAsset(t, service, source.ID, "lead@example.com", "ready for deterministic processing")

	got, err := service.Process(context.Background(), ProcessRequest{
		MediaID:          source.ID,
		Resize:           ResizeOptions{MaxWidth: 600, MaxHeight: 600},
		Format:           "image/webp",
		RemoveBackground: true,
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	assertProcessedStubAsset(t, got, source.ID)
}

func TestQualityAssessmentCoversResolutionAspectFormatAltAndBrandSafety(t *testing.T) {
	t.Parallel()

	service := NewService(ServiceConfig{})
	got := service.AssessQuality(Asset{
		Metadata: Metadata{
			MimeType:      "image/bmp",
			ContentLength: 6 * 1024 * 1024,
			Width:         320,
			Height:        200,
		},
		AltText: "product image",
	})

	if got.Pass {
		t.Fatal("quality assessment passed, want failures")
	}
	for _, want := range []string{"resolution_too_small", "aspect_ratio_out_of_range", "unsupported_format", "alt_text_generic", "brand_safety_pending"} {
		if !containsIssue(got.Issues, want) {
			t.Fatalf("issues = %#v, missing %q", got.Issues, want)
		}
	}
	assertGoldenQualityReport(t, got)
}

func TestServiceStoresProcessedAssetInObjectStore(t *testing.T) {
	t.Parallel()

	store := &recordingStore{}
	service := NewService(ServiceConfig{HTTPClient: fixedImageClient(t), Store: store})
	source, err := service.Source(context.Background(), SourceRequest{
		URL:       "https://supplier.example/images/lamp.png",
		ProductID: "product-123",
		AltText:   "Matte black desk lamp on white background",
	})
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	stored, err := service.Store(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	if stored.Storage.Key == "" || !strings.Contains(stored.Storage.Key, "product-123") {
		t.Fatalf("storage key = %q", stored.Storage.Key)
	}
	if store.body == "" || store.contentType != "image/png" {
		t.Fatalf("stored body/content type = %q/%q", store.body, store.contentType)
	}
}

func TestServiceValidateUpdatesStoredQualityAndReportsMissingAssets(t *testing.T) {
	t.Parallel()

	service := NewService(ServiceConfig{HTTPClient: fixedImageClient(t)})
	source, err := service.Source(context.Background(), SourceRequest{
		URL:     "https://supplier.example/images/lamp.png",
		AltText: "Matte black desk lamp on white background",
	})
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	qa, err := service.Validate(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if qa.Pass || !containsIssue(qa.Issues, "resolution_too_small") {
		t.Fatalf("qa = %+v, want resolution failure for tiny fixture", qa)
	}
	updated, ok := service.Get(source.ID)
	if !ok || updated.Quality.Score != qa.Score {
		t.Fatalf("updated asset = %+v, ok=%v", updated, ok)
	}
	if _, err := service.Validate(context.Background(), "missing"); !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("Validate missing err = %v", err)
	}
}

func TestServiceRejectsInvalidSourceAndUnavailableClient(t *testing.T) {
	t.Parallel()

	service := NewService(ServiceConfig{})
	if _, err := service.Source(context.Background(), SourceRequest{URL: "file:///tmp/image.png"}); !errors.Is(err, ErrInvalidSourceURL) {
		t.Fatalf("invalid URL err = %v", err)
	}
	if _, err := service.Source(context.Background(), SourceRequest{URL: "https://supplier.example/image.png"}); !errors.Is(err, ErrHTTPClientRequired) {
		t.Fatalf("missing client err = %v", err)
	}
}

func TestServiceResizesLargeImageMetadataAndNormalizesFormats(t *testing.T) {
	t.Parallel()

	service := NewService(ServiceConfig{})
	service.save(Asset{
		ID:           "media_source",
		ProductID:    "p1",
		Metadata:     Metadata{MimeType: "image/jpeg", ContentLength: 4, ChecksumSHA256: strings.Repeat("a", 64), Width: 1200, Height: 900},
		AltText:      "Studio product photo with clear context",
		ReviewState:  MediaReviewStateApproved,
		ProcessState: MediaProcessStatePending,
		payload:      []byte("jpeg"),
	})
	got, err := service.Process(context.Background(), ProcessRequest{
		MediaID: "media_source",
		Resize:  ResizeOptions{MaxWidth: 600, MaxHeight: 600},
		Format:  "webp",
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got.Metadata.Width != 600 || got.Metadata.Height != 450 || got.Metadata.MimeType != "image/webp" {
		t.Fatalf("processed metadata = %+v", got.Metadata)
	}
}

func TestServiceSourcesGoldenMetadataAndAltTextValidation(t *testing.T) {
	t.Parallel()

	service := NewService(ServiceConfig{HTTPClient: fixedImageClient(t)})
	got, err := service.Source(context.Background(), SourceRequest{
		URL:       "https://supplier.example/images/lamp.png",
		ProductID: "product-123",
		AltText:   "product image",
	})
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	assertGoldenMetadata(t, got.Metadata)

	qa, err := service.Validate(context.Background(), got.ID)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !containsIssue(qa.Issues, "alt_text_generic") {
		t.Fatalf("qa issues = %#v, want alt text validation issue", qa.Issues)
	}
}

func TestServiceSourceIsIdempotentAndSeedsPendingLifecycleState(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 5, 17, 14, 4, 17, 0, time.FixedZone("AEST", 10*60*60))
	replayedAt := startedAt.Add(2 * time.Minute)
	timestamps := []time.Time{startedAt, replayedAt}
	service := NewService(ServiceConfig{
		HTTPClient: fixedImageClient(t),
		Now: func() time.Time {
			next := timestamps[0]
			timestamps = timestamps[1:]
			return next
		},
	})

	first, err := service.Source(context.Background(), SourceRequest{
		URL:       "https://supplier.example/images/lamp.png",
		ProductID: "product-123",
		AltText:   "Matte black desk lamp on white background",
	})
	if err != nil {
		t.Fatalf("first Source: %v", err)
	}
	second, err := service.Source(context.Background(), SourceRequest{
		URL:       "https://supplier.example/images/lamp.png",
		ProductID: "product-123",
		AltText:   "Matte black desk lamp on white background",
	})
	if err != nil {
		t.Fatalf("second Source: %v", err)
	}
	assertIdempotentSourceReplay(t, first, second, startedAt)
}

func TestServiceProcessRejectsAssetsThatHaveNotBeenApproved(t *testing.T) {
	t.Parallel()

	service := NewService(ServiceConfig{HTTPClient: fixedImageClient(t)})
	source, err := service.Source(context.Background(), SourceRequest{
		URL:       "https://supplier.example/images/lamp.png",
		ProductID: "product-123",
		AltText:   "Matte black desk lamp on white background",
	})
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	if _, err := service.Process(context.Background(), ProcessRequest{MediaID: source.ID}); err == nil {
		t.Fatal("Process err = nil, want lifecycle rejection for unapproved asset")
	}
}

func BenchmarkMediaQAValidate(b *testing.B) {
	service := NewService(ServiceConfig{})
	service.save(Asset{
		ID:        "media_benchmark",
		ProductID: "product-123",
		AltText:   "Studio product photo with matte black desk lamp",
		Metadata: Metadata{
			MimeType:       "image/webp",
			ContentLength:  512_000,
			ChecksumSHA256: strings.Repeat("b", 64),
			Width:          1200,
			Height:         900,
		},
		payload: []byte("webp-benchmark"),
	})

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		qa, err := service.Validate(context.Background(), "media_benchmark")
		if err != nil {
			b.Fatalf("Validate: %v", err)
		}
		if !qa.Pass {
			b.Fatalf("qa failed: %#v", qa)
		}
	}
}

func assetJSONFields(t *testing.T, asset Asset) map[string]any {
	t.Helper()
	raw, err := json.Marshal(asset)
	if err != nil {
		t.Fatalf("marshal asset: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode asset fields: %v", err)
	}
	return fields
}

func mustApproveAsset(t *testing.T, service *Service, mediaID, reviewer, note string) {
	t.Helper()
	if _, err := service.Approve(context.Background(), mediaID, ReviewRequest{
		Reviewer: reviewer,
		Note:     note,
	}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
}

func assertProcessedStubAsset(t *testing.T, got Asset, sourceID string) {
	t.Helper()
	assertProcessedAssetIdentity(t, got, sourceID)
	assertProcessedAssetOperations(t, got.Processing.Operations)
}

func assertIdempotentSourceReplay(t *testing.T, first, second Asset, startedAt time.Time) {
	t.Helper()
	assertSourceReplayIdentity(t, first, second, startedAt)
	assertLifecycleJSONFields(t, second)
}

func assertProcessedAssetIdentity(t *testing.T, got Asset, sourceID string) {
	t.Helper()
	if got.ID == sourceID {
		t.Fatal("processed asset should have a distinct id")
	}
	if got.Metadata.MimeType != "image/webp" {
		t.Fatalf("mime type = %q", got.Metadata.MimeType)
	}
	if got.Metadata.Width != 1 || got.Metadata.Height != 1 {
		t.Fatalf("dimensions = %dx%d, want source dimensions preserved for small image", got.Metadata.Width, got.Metadata.Height)
	}
}

func assertProcessedAssetOperations(t *testing.T, operations []ProcessingOperation) {
	t.Helper()
	for _, want := range []string{"resize_stub", "format_conversion_stub", "background_removal_todo"} {
		if !containsOperation(operations, want) {
			t.Fatalf("operations = %#v, missing %q", operations, want)
		}
	}
}

func assertSourceReplayIdentity(t *testing.T, first, second Asset, startedAt time.Time) {
	t.Helper()
	if second.ID != first.ID {
		t.Fatalf("duplicate source id = %q, want %q", second.ID, first.ID)
	}
	if !first.CreatedAt.Equal(startedAt) {
		t.Fatalf("first created_at = %s, want %s", first.CreatedAt, startedAt)
	}
	if !second.CreatedAt.Equal(startedAt) {
		t.Fatalf("duplicate created_at = %s, want original %s", second.CreatedAt, startedAt)
	}
}

func assertLifecycleJSONFields(t *testing.T, asset Asset) {
	t.Helper()
	fields := assetJSONFields(t, asset)
	assertJSONFieldEquals(t, fields, "review_state", "pending")
	assertJSONFieldEquals(t, fields, "process_state", "pending")
	assertJSONFieldPresent(t, fields, "updated_at")
}

func assertJSONFieldEquals(t *testing.T, fields map[string]any, key string, want any) {
	t.Helper()
	if got := fields[key]; got != want {
		t.Fatalf("%s = %#v, want %#v; fields=%v", key, got, want, fields)
	}
}

func assertJSONFieldPresent(t *testing.T, fields map[string]any, key string) {
	t.Helper()
	if fields[key] == nil {
		t.Fatalf("%s missing from asset payload; fields=%v", key, fields)
	}
}

func assertGoldenMetadata(t *testing.T, got Metadata) {
	t.Helper()
	var want Metadata
	raw, err := goldenMetadata.ReadFile("testdata/source_metadata.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if got != want {
		t.Fatalf("metadata = %+v, want %+v", got, want)
	}
}

func assertGoldenQualityReport(t *testing.T, got QualityReport) {
	t.Helper()
	var want QualityReport
	raw, err := goldenMetadata.ReadFile("testdata/quality_report.golden.json")
	if err != nil {
		t.Fatalf("read golden quality: %v", err)
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode golden quality: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("quality report = %+v, want %+v", got, want)
	}
}

func fixedImageClient(t *testing.T) HTTPClient {
	t.Helper()
	return roundTripClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(strings.NewReader(onePixelPNGString())),
		}, nil
	})
}

func onePixelPNGString() string {
	raw, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	return string(raw)
}

type roundTripClient func(*http.Request) (*http.Response, error)

func (c roundTripClient) Do(req *http.Request) (*http.Response, error) {
	return c(req)
}

type recordingStore struct {
	key         string
	contentType string
	body        string
}

func (s *recordingStore) Put(_ context.Context, object MediaObject) (StoredMediaObject, error) {
	body, err := io.ReadAll(object.Body)
	if err != nil {
		return StoredMediaObject{}, err
	}
	s.key = object.Key
	s.contentType = object.ContentType
	s.body = string(body)
	return StoredMediaObject{Key: object.Key, URL: "/media/" + object.Key, ContentType: object.ContentType, SizeBytes: int64(len(body))}, nil
}

func (s *recordingStore) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, nil
}

func (s *recordingStore) Delete(context.Context, string) error {
	return nil
}

func containsIssue(issues []QualityIssue, id string) bool {
	for _, issue := range issues {
		if issue.ID == id {
			return true
		}
	}
	return false
}

func containsOperation(ops []ProcessingOperation, id string) bool {
	for _, op := range ops {
		if op.ID == id {
			return true
		}
	}
	return false
}

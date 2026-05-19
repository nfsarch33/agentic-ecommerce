package main

import (
	"reflect"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/media/intelligence"
)

func TestMediaOpenAPIContracts(t *testing.T) {
	t.Parallel()

	spec := loadOpenAPISpec(t)
	paths := specMap(t, spec, "paths")

	assertOperation(t, paths, "/api/v1/media", "get", "listMedia", []string{"200", "503"})
	assertOperation(t, paths, "/api/v1/media/source", "post", "sourceMedia", []string{"200", "201", "400", "422", "502", "503"})
	assertOperation(t, paths, "/api/v1/media/process", "post", "processMedia", []string{"200", "400", "404", "409", "422"})
	assertOperation(t, paths, "/api/v1/media/{id}", "get", "getMedia", []string{"200", "404"})
	assertOperation(t, paths, "/api/v1/media/{id}/validate", "post", "validateMedia", []string{"200", "404"})
	assertOperation(t, paths, "/api/v1/media/{id}/approve", "post", "approveMedia", []string{"200", "400", "404", "409", "422"})
	assertOperation(t, paths, "/api/v1/media/{id}/reject", "post", "rejectMedia", []string{"200", "400", "404", "409", "422"})

	schemas := specMap(t, specMap(t, spec, "components"), "schemas")
	assertRequiredFields(t, schemas, "MediaSourceRequest", []string{"url"})
	assertRequiredFields(t, schemas, "MediaProcessRequest", []string{"media_id"})
	assertRequiredFields(t, schemas, "MediaApproveRequest", []string{"reviewer"})
	assertRequiredFields(t, schemas, "MediaRejectRequest", []string{"reviewer", "note"})
	assertRequiredFields(t, schemas, "MediaAssetListResponse", []string{"assets"})
	assertRequiredFields(t, schemas, "MediaMetadata", jsonFieldNames(reflect.TypeOf(intelligence.Metadata{})))
	assertRequiredFields(t, schemas, "MediaQualityReport", []string{"pass", "score"})
	assertRequiredFields(t, schemas, "MediaAsset", []string{"id", "metadata", "review_state", "process_state", "created_at", "updated_at"})
	assertSchemaHasProperties(t, schemas, "MediaAsset", []string{"id", "product_id", "source_url", "alt_text", "metadata", "processing", "quality", "storage", "created_at", "updated_at", "review_state", "process_state", "review_note", "reviewed_at", "reviewer"})
	assertSchemaHasProperties(t, schemas, "MediaAssetListResponse", []string{"assets"})
	assertSchemaHasProperties(t, schemas, "MediaQualityIssue", jsonFieldNames(reflect.TypeOf(intelligence.QualityIssue{})))
	assertEnum(t, schemas, "MediaProcessRequest", "format", []string{"gif", "image/gif", "image/jpeg", "image/png", "image/webp", "jpeg", "png", "webp"})
	assertEnum(t, schemas, "MediaAsset", "review_state", []string{"approved", "pending", "rejected"})
	assertEnum(t, schemas, "MediaAsset", "process_state", []string{"pending", "processed"})
}

func assertSchemaHasProperties(t *testing.T, schemas map[string]any, schemaName string, fields []string) {
	t.Helper()
	properties := specMap(t, specMap(t, schemas, schemaName), "properties")
	for _, field := range fields {
		if _, ok := properties[field]; !ok {
			t.Fatalf("%s properties missing %q; properties=%v", schemaName, field, properties)
		}
	}
}

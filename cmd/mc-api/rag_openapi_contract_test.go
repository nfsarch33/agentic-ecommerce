package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRAGAndFactCheckOpenAPIContracts(t *testing.T) {
	t.Parallel()

	spec := loadOpenAPISpec(t)
	paths := specMap(t, spec, "paths")

	assertOperation(t, paths, "/api/v1/rag/documents", "post", "ingestRAGDocument", []string{"201", "400", "422", "503", "504"})
	assertOperation(t, paths, "/api/v1/rag/search", "get", "searchRAGEvidence", []string{"200", "400", "503", "504"})
	assertOperation(t, paths, "/api/v1/content/generate", "post", "generateFactCheckedContent", []string{"200", "400", "404", "502", "503", "504"})
	assertOperation(t, paths, "/api/v1/content/fact-checks/{id}", "get", "getFactCheckResult", []string{"200", "404"})

	schemas := specMap(t, specMap(t, spec, "components"), "schemas")
	assertRequiredFields(t, schemas, "RAGIngestResponse", jsonFieldNames(reflect.TypeOf(ragIngestResponse{})))
	assertRequiredFields(t, schemas, "RAGSearchResponse", jsonFieldNames(reflect.TypeOf(ragSearchResponse{})))
	assertRequiredFields(t, schemas, "ContentFactCheckResponse", jsonFieldNames(reflect.TypeOf(contentFactCheckResponse{})))
	assertEnum(t, schemas, "ClaimCheck", "status", []string{"supported", "unsupported", "contradicted", "ambiguous"})
}

func loadOpenAPISpec(t *testing.T) map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "api", "openapi.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}
	var spec map[string]any
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("parse openapi spec: %v", err)
	}
	return spec
}

func assertOperation(t *testing.T, paths map[string]any, path, method, operationID string, statuses []string) {
	t.Helper()
	operation := specMap(t, specMap(t, paths, path), method)
	if got := specString(t, operation, "operationId"); got != operationID {
		t.Fatalf("%s %s operationId = %q, want %q", method, path, got, operationID)
	}
	responses := specMap(t, operation, "responses")
	for _, status := range statuses {
		if _, ok := responses[status]; !ok {
			t.Fatalf("%s %s missing response status %s", method, path, status)
		}
	}
}

func assertRequiredFields(t *testing.T, schemas map[string]any, schemaName string, fields []string) {
	t.Helper()
	schema := specMap(t, schemas, schemaName)
	requiredValues, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("%s required fields missing or invalid", schemaName)
	}
	required := make(map[string]bool, len(requiredValues))
	for _, value := range requiredValues {
		required[value.(string)] = true
	}
	for _, field := range fields {
		if !required[field] {
			t.Fatalf("%s required fields missing %q; required=%v", schemaName, field, requiredValues)
		}
	}
}

func assertEnum(t *testing.T, schemas map[string]any, schemaName, field string, want []string) {
	t.Helper()
	properties := specMap(t, specMap(t, schemas, schemaName), "properties")
	enumValues, ok := specMap(t, properties, field)["enum"].([]any)
	if !ok {
		t.Fatalf("%s.%s enum missing or invalid", schemaName, field)
	}
	got := make([]string, len(enumValues))
	for i, value := range enumValues {
		got[i] = value.(string)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s.%s enum = %v, want %v", schemaName, field, got, want)
	}
}

func jsonFieldNames(typ reflect.Type) []string {
	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name != "" && name != "-" {
			fields = append(fields, name)
		}
	}
	return fields
}

func specMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("spec key %q missing or not a map", key)
	}
	return value
}

func specString(t *testing.T, parent map[string]any, key string) string {
	t.Helper()
	value, ok := parent[key].(string)
	if !ok {
		t.Fatalf("spec key %q missing or not a string", key)
	}
	return value
}

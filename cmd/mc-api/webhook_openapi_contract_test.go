package main

import (
	"sort"
	"strings"
	"testing"
)

func TestWebhookOpenAPIContracts(t *testing.T) {
	t.Parallel()

	spec := loadOpenAPISpec(t)
	paths := specMap(t, spec, "paths")

	assertOperation(t, paths, "/api/v1/webhooks", "post", "createWebhook", []string{"201", "400", "401", "403", "422"})
	assertOperation(t, paths, "/api/v1/webhooks", "get", "listWebhooks", []string{"200", "401", "403"})
	assertOperation(t, paths, "/api/v1/webhooks/{id}", "delete", "deleteWebhook", []string{"204", "401", "403", "404"})
	assertOperation(t, paths, "/api/v1/webhooks/{id}/test", "post", "testWebhook", []string{"202", "400", "401", "403", "404", "422"})
	assertRequestSchema(t, paths, "/api/v1/webhooks", "post", "CreateWebhookRequest")
	assertResponseSchema(t, paths, "/api/v1/webhooks", "post", "201", "WebhookRegistration")
	assertResponseSchema(t, paths, "/api/v1/webhooks", "get", "200", "WebhookListResponse")
	assertRequestSchema(t, paths, "/api/v1/webhooks/{id}/test", "post", "TestWebhookRequest")
	assertResponseSchema(t, paths, "/api/v1/webhooks/{id}/test", "post", "202", "WebhookTestResponse")

	schemas := specMap(t, specMap(t, spec, "components"), "schemas")
	assertRequiredFields(t, schemas, "CreateWebhookRequest", []string{"url", "event_types", "secret"})
	assertRequiredFields(t, schemas, "WebhookRegistration", []string{"id", "url", "event_types", "secret_hash", "enabled", "created_at"})
	assertRequiredFields(t, schemas, "WebhookListResponse", []string{"webhooks"})
	assertRequiredFields(t, schemas, "WebhookTestResponse", []string{"delivery"})
	assertRequiredFields(t, schemas, "WebhookDeliveryResult", []string{"webhook_id", "event_id", "event_type", "success", "status", "attempts", "created_at"})
	assertArrayEnum(t, schemas, "CreateWebhookRequest", "event_types", []string{"agent.run.completed", "compliance.checked", "order.placed", "product.created", "product.updated", "sync.completed"})
	assertNoSchemaProperties(t, schemas, "WebhookRegistration", []string{"secret"})
	assertNoSchemaProperties(t, schemas, "WebhookDeliveryResult", []string{"secret", "url"})
}

func assertRequestSchema(t *testing.T, paths map[string]any, path, method, schemaName string) {
	t.Helper()
	operation := specMap(t, specMap(t, paths, path), method)
	requestBody := specMap(t, operation, "requestBody")
	content := specMap(t, requestBody, "content")
	schema := specMap(t, specMap(t, content, "application/json"), "schema")
	assertRef(t, schema, schemaName)
}

func assertResponseSchema(t *testing.T, paths map[string]any, path, method, status, schemaName string) {
	t.Helper()
	operation := specMap(t, specMap(t, paths, path), method)
	responses := specMap(t, operation, "responses")
	response := specMap(t, responses, status)
	content := specMap(t, response, "content")
	schema := specMap(t, specMap(t, content, "application/json"), "schema")
	assertRef(t, schema, schemaName)
}

func assertRef(t *testing.T, schema map[string]any, schemaName string) {
	t.Helper()
	want := "#/components/schemas/" + schemaName
	got, ok := schema["$ref"].(string)
	if !ok || got != want {
		t.Fatalf("schema ref = %q, want %q", got, want)
	}
}

func assertNoSchemaProperties(t *testing.T, schemas map[string]any, schemaName string, fields []string) {
	t.Helper()
	properties := specMap(t, specMap(t, schemas, schemaName), "properties")
	for _, field := range fields {
		if _, ok := properties[field]; ok {
			t.Fatalf("%s must not expose property %q", schemaName, field)
		}
	}
}

func assertArrayEnum(t *testing.T, schemas map[string]any, schemaName, field string, want []string) {
	t.Helper()
	properties := specMap(t, specMap(t, schemas, schemaName), "properties")
	items := specMap(t, specMap(t, properties, field), "items")
	enumValues, ok := items["enum"].([]any)
	if !ok {
		t.Fatalf("%s.%s items enum missing or invalid", schemaName, field)
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

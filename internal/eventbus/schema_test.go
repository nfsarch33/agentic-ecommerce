package eventbus

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// PluginInstalledV1 is the canonical v1 payload shape registered with
// the schema registry below. v2 in this test adds new optional fields;
// v1 envelopes must still decode after v2 is registered.
type PluginInstalledV1 struct {
	TenantID string `json:"tenant_id"`
	Slug     string `json:"slug"`
	Version  string `json:"version"`
}

type PluginInstalledV2 struct {
	TenantID string `json:"tenant_id"`
	Slug     string `json:"slug"`
	Version  string `json:"version"`
	Vendor   string `json:"vendor"`
}

func TestRegisterAndDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	r := NewSchemaRegistry()
	mustRegister(t, r, "marketplace.plugin.installed", 1, decoderForV1)
	emitted := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	env := EventEnvelope[PluginInstalledV1]{
		Schema:    "marketplace.plugin.installed",
		Version:   1,
		TenantID:  "tenant-a",
		EmittedAt: emitted,
		Payload: PluginInstalledV1{
			TenantID: "tenant-a",
			Slug:     "stripe",
			Version:  "1.2.3",
		},
	}
	raw, err := MarshalEnvelope(env)
	if err != nil {
		t.Fatalf("MarshalEnvelope: %v", err)
	}
	gotEnv, gotPayload, err := r.DecodeEnvelope(raw)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if gotEnv.Schema != env.Schema {
		t.Fatalf("Schema = %q, want %q", gotEnv.Schema, env.Schema)
	}
	if gotEnv.Version != 1 {
		t.Fatalf("Version = %d, want 1", gotEnv.Version)
	}
	got, ok := gotPayload.(PluginInstalledV1)
	if !ok {
		t.Fatalf("decoded payload type = %T", gotPayload)
	}
	if got.Slug != env.Payload.Slug {
		t.Fatalf("payload slug = %q, want %q", got.Slug, env.Payload.Slug)
	}
}

// TestBackwardCompatibility is the spec-required smoke test: v1
// envelopes must still decode without breaking v2 consumers.
func TestBackwardCompatibility(t *testing.T) {
	t.Parallel()
	r := NewSchemaRegistry()
	mustRegister(t, r, "marketplace.plugin.installed", 1, decoderForV1)

	v1Env := EventEnvelope[PluginInstalledV1]{
		Schema:    "marketplace.plugin.installed",
		Version:   1,
		TenantID:  "tenant-a",
		EmittedAt: time.Now().UTC(),
		Payload:   PluginInstalledV1{TenantID: "tenant-a", Slug: "stripe", Version: "1.0.0"},
	}
	raw, err := MarshalEnvelope(v1Env)
	if err != nil {
		t.Fatalf("MarshalEnvelope v1: %v", err)
	}

	// Now register v2 and verify v1 still decodes.
	mustRegister(t, r, "marketplace.plugin.installed", 2, decoderForV2)
	gotEnv, gotPayload, err := r.DecodeEnvelope(raw)
	if err != nil {
		t.Fatalf("v1 envelope should still decode: %v", err)
	}
	if gotEnv.Version != 1 {
		t.Fatalf("v1 envelope decoded with version %d", gotEnv.Version)
	}
	if _, ok := gotPayload.(PluginInstalledV1); !ok {
		t.Fatalf("v1 payload decoded as wrong type %T", gotPayload)
	}
}

func TestUnknownSchemaErrors(t *testing.T) {
	t.Parallel()
	r := NewSchemaRegistry()
	raw := []byte(`{"schema":"ghost","version":1,"tenant_id":"t","emitted_at":"2026-05-08T00:00:00Z","payload":{}}`)
	_, _, err := r.DecodeEnvelope(raw)
	if !errors.Is(err, ErrSchemaNotRegistered) {
		t.Fatalf("expected ErrSchemaNotRegistered, got %v", err)
	}
}

func TestUnknownVersionErrors(t *testing.T) {
	t.Parallel()
	r := NewSchemaRegistry()
	mustRegister(t, r, "marketplace.plugin.installed", 1, decoderForV1)
	raw := []byte(`{"schema":"marketplace.plugin.installed","version":99,"tenant_id":"t","emitted_at":"2026-05-08T00:00:00Z","payload":{}}`)
	_, _, err := r.DecodeEnvelope(raw)
	if !errors.Is(err, ErrSchemaVersionUnsupported) {
		t.Fatalf("expected ErrSchemaVersionUnsupported, got %v", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	t.Parallel()
	r := NewSchemaRegistry()
	if err := r.RegisterSchema("", 1, decoderForV1); !errors.Is(err, ErrSchemaNotRegistered) {
		t.Fatalf("empty name should error, got %v", err)
	}
	if err := r.RegisterSchema("x", 0, decoderForV1); !errors.Is(err, ErrSchemaVersionUnsupported) {
		t.Fatalf("zero version should error, got %v", err)
	}
	if err := r.RegisterSchema("x", 1, nil); !errors.Is(err, ErrSchemaNotRegistered) {
		t.Fatalf("nil decoder should error, got %v", err)
	}
}

func TestVersionsListsAscending(t *testing.T) {
	t.Parallel()
	r := NewSchemaRegistry()
	mustRegister(t, r, "x", 3, decoderForV1)
	mustRegister(t, r, "x", 1, decoderForV1)
	mustRegister(t, r, "x", 2, decoderForV1)
	got := r.Versions("x")
	want := []int{1, 2, 3}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("Versions = %v, want %v", got, want)
		}
	}
	if r.Versions("ghost") != nil {
		t.Fatalf("Versions for unknown schema should be nil")
	}
}

func TestHasSchema(t *testing.T) {
	t.Parallel()
	r := NewSchemaRegistry()
	mustRegister(t, r, "x", 1, decoderForV1)
	if !r.HasSchema("x", 1) {
		t.Fatalf("HasSchema should report registered")
	}
	if r.HasSchema("x", 2) {
		t.Fatalf("HasSchema should not report unregistered version")
	}
	if r.HasSchema("y", 1) {
		t.Fatalf("HasSchema should not report unregistered schema")
	}
}

func TestMarshalEnvelopeDefaults(t *testing.T) {
	t.Parallel()
	env := EventEnvelope[PluginInstalledV1]{
		Schema:  "marketplace.plugin.installed",
		Payload: PluginInstalledV1{TenantID: "t", Slug: "stripe", Version: "1.0.0"},
		// Version intentionally zero, EmittedAt zero.
	}
	raw, err := MarshalEnvelope(env)
	if err != nil {
		t.Fatalf("MarshalEnvelope: %v", err)
	}
	if len(raw) == 0 {
		t.Fatalf("encoded envelope is empty")
	}
}

func TestDecodeEnvelopeMalformedJSON(t *testing.T) {
	t.Parallel()
	r := NewSchemaRegistry()
	if _, _, err := r.DecodeEnvelope([]byte("garbage")); err == nil {
		t.Fatalf("expected JSON decode error")
	}
}

func TestDecodeEnvelopeDecoderError(t *testing.T) {
	t.Parallel()
	r := NewSchemaRegistry()
	mustRegister(t, r, "x", 1, func(_ []byte) (any, error) {
		return nil, errors.New("decoder boom")
	})
	raw := []byte(`{"schema":"x","version":1,"tenant_id":"t","emitted_at":"2026-05-08T00:00:00Z","payload":{}}`)
	if _, _, err := r.DecodeEnvelope(raw); err == nil {
		t.Fatalf("expected decoder error to surface")
	}
}

func mustRegister(t *testing.T, r *SchemaRegistry, name EventName, version int, decoder SchemaDecoder) {
	t.Helper()
	if err := r.RegisterSchema(name, version, decoder); err != nil {
		t.Fatalf("RegisterSchema %s@%d: %v", name, version, err)
	}
}

func decoderForV1(raw []byte) (any, error) {
	var p PluginInstalledV1
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return p, nil
}

func decoderForV2(raw []byte) (any, error) {
	var p PluginInstalledV2
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return p, nil
}

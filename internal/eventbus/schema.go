package eventbus

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// EventName is the typed identifier of an event in the schema
// registry. Marketplace plugin manifests use this same type via
// internal/marketplace.EventName so producers and consumers share a
// single naming surface across the v2.4.0 plugin framework.
//
// We keep EventName separate from EventType (the legacy enum used by
// in-process publishers) so the versioned schema registry can evolve
// without forcing every legacy publisher to migrate at once.
type EventName string

// EventEnvelope[T] is the typed wrapper every v2.4.0+ event ships
// inside. It pairs a payload with the schema version, a tenant id,
// and an emit timestamp. The generic parameter avoids the
// `interface{}` hop that legacy MembershipPayload-as-map[string]any
// uses.
type EventEnvelope[T any] struct {
	Schema    EventName `json:"schema"`
	Version   int       `json:"version"`
	TenantID  string    `json:"tenant_id"`
	EmittedAt time.Time `json:"emitted_at"`
	Payload   T         `json:"payload"`
}

// SchemaDecoder decodes the JSON payload bytes for a given schema +
// version into a structured value. The registry picks the right
// decoder via (schema, version).
type SchemaDecoder func([]byte) (any, error)

// SchemaRegistry is the central store for versioned event schemas.
// Plugins consume only schemas they explicitly subscribed to, and the
// registry enforces backward compatibility by keeping every previous
// version available.
type SchemaRegistry struct {
	mu       sync.RWMutex
	versions map[EventName]map[int]SchemaDecoder
}

// ErrSchemaNotRegistered is returned when DecodeEnvelope is asked
// to handle an unknown (schema, version).
var ErrSchemaNotRegistered = errors.New("schema not registered")

// ErrSchemaVersionUnsupported is returned by DecodeEnvelope when the
// schema is registered but the requested version is not.
var ErrSchemaVersionUnsupported = errors.New("schema version unsupported")

// NewSchemaRegistry returns an empty registry.
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{versions: make(map[EventName]map[int]SchemaDecoder)}
}

// RegisterSchema records a decoder for (name, version). Calling twice
// with the same pair overwrites the prior decoder; this lets test
// harnesses substitute in fakes without rebuilding the registry.
func (r *SchemaRegistry) RegisterSchema(name EventName, version int, decoder SchemaDecoder) error {
	if name == "" {
		return fmt.Errorf("%w: empty event name", ErrSchemaNotRegistered)
	}
	if version <= 0 {
		return fmt.Errorf("%w: version must be >0 for %s", ErrSchemaVersionUnsupported, name)
	}
	if decoder == nil {
		return fmt.Errorf("%w: nil decoder for %s@%d", ErrSchemaNotRegistered, name, version)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.versions[name]; !ok {
		r.versions[name] = make(map[int]SchemaDecoder)
	}
	r.versions[name][version] = decoder
	return nil
}

// HasSchema reports whether (name, version) is registered.
func (r *SchemaRegistry) HasSchema(name EventName, version int) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions, ok := r.versions[name]
	if !ok {
		return false
	}
	_, ok = versions[version]
	return ok
}

// Versions returns the registered version numbers for a schema in
// ascending order. Empty for unknown schemas.
func (r *SchemaRegistry) Versions(name EventName) []int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions, ok := r.versions[name]
	if !ok {
		return nil
	}
	out := make([]int, 0, len(versions))
	for v := range versions {
		out = append(out, v)
	}
	sortInts(out)
	return out
}

// DecodeEnvelope unmarshals raw bytes into a generic envelope and
// dispatches the payload to the registered decoder for
// (envelope.Schema, envelope.Version). The returned value is the
// concrete payload type the decoder produced.
func (r *SchemaRegistry) DecodeEnvelope(raw []byte) (rawEnvelope, any, error) {
	var env rawEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return rawEnvelope{}, nil, fmt.Errorf("decode envelope: %w", err)
	}
	r.mu.RLock()
	versions, schemaOK := r.versions[env.Schema]
	r.mu.RUnlock()
	if !schemaOK {
		return env, nil, fmt.Errorf("%w: %s", ErrSchemaNotRegistered, env.Schema)
	}
	decoder, ok := versions[env.Version]
	if !ok {
		return env, nil, fmt.Errorf("%w: %s@%d", ErrSchemaVersionUnsupported, env.Schema, env.Version)
	}
	payload, err := decoder(env.Payload)
	if err != nil {
		return env, nil, fmt.Errorf("decode %s@%d: %w", env.Schema, env.Version, err)
	}
	return env, payload, nil
}

// rawEnvelope is the on-the-wire shape used by DecodeEnvelope before
// the payload bytes are dispatched to a typed decoder.
type rawEnvelope struct {
	Schema    EventName       `json:"schema"`
	Version   int             `json:"version"`
	TenantID  string          `json:"tenant_id"`
	EmittedAt time.Time       `json:"emitted_at"`
	Payload   json.RawMessage `json:"payload"`
}

// MarshalEnvelope is the canonical encoder. Generic over T so callers
// keep typed payloads end to end.
func MarshalEnvelope[T any](env EventEnvelope[T]) ([]byte, error) {
	if env.EmittedAt.IsZero() {
		env.EmittedAt = time.Now().UTC()
	}
	if env.Version == 0 {
		env.Version = 1
	}
	return json.Marshal(env)
}

// sortInts sorts a slice of ints ascending without pulling in
// sort.Slice's reflect overhead.
func sortInts(values []int) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
}

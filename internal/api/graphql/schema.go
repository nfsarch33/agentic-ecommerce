package graphql

import (
	"errors"
	"strings"
	"sync"
)

var (
	ErrTypeNotFound  = errors.New("type not found")
	ErrUnknownField  = errors.New("unknown field")
	ErrDepthExceeded = errors.New("query depth exceeded")
)

type FieldDef struct {
	Name string
	Type string
}

type TypeDefs struct {
	mu    sync.RWMutex
	types map[string][]FieldDef
}

func NewTypeDefs() *TypeDefs {
	return &TypeDefs{types: make(map[string][]FieldDef)}
}

func (td *TypeDefs) Register(typeName string, fields []FieldDef) {
	td.mu.Lock()
	defer td.mu.Unlock()
	td.types[typeName] = fields
}

func (td *TypeDefs) Fields(typeName string) ([]FieldDef, error) {
	td.mu.RLock()
	defer td.mu.RUnlock()
	f, ok := td.types[typeName]
	if !ok {
		return nil, ErrTypeNotFound
	}
	return f, nil
}

type ResolverFunc func(args map[string]any) (any, error)

type Resolvers struct {
	mu        sync.RWMutex
	resolvers map[string]ResolverFunc
}

func NewResolvers() *Resolvers {
	return &Resolvers{resolvers: make(map[string]ResolverFunc)}
}

func (r *Resolvers) Register(field string, fn ResolverFunc) {
	r.mu.Lock()
	r.resolvers[field] = fn
	r.mu.Unlock()
}

func (r *Resolvers) Resolve(field string, args map[string]any) (any, error) {
	r.mu.RLock()
	fn, ok := r.resolvers[field]
	r.mu.RUnlock()
	if !ok {
		return nil, ErrUnknownField
	}
	return fn(args)
}

// QueryComplexity counts the total number of field selections (simplified).
func QueryComplexity(query string) int {
	count := 0
	for _, line := range strings.Split(query, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "}") {
			count++
		}
	}
	return count
}

// DepthLimit returns error if the query nesting exceeds max.
func DepthLimit(query string, max int) error {
	depth, maxDepth := 0, 0
	for _, ch := range query {
		if ch == '{' {
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
		} else if ch == '}' {
			depth--
		}
	}
	if maxDepth > max {
		return ErrDepthExceeded
	}
	return nil
}

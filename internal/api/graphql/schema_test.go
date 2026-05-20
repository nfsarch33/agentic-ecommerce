package graphql_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/api/graphql"
)

func TestGraphQL_RegisterTypeDefs(t *testing.T) {
	t.Parallel()
	td := graphql.NewTypeDefs()
	td.Register("Product", []graphql.FieldDef{{Name: "id", Type: "String"}, {Name: "name", Type: "String"}})
	fields, err := td.Fields("Product")
	if err != nil {
		t.Fatalf("Fields: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
}

func TestGraphQL_ResolveSimpleQuery(t *testing.T) {
	t.Parallel()
	r := graphql.NewResolvers()
	r.Register("product", func(args map[string]any) (any, error) { return "ProductA", nil })
	result, err := r.Resolve("product", nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result != "ProductA" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestGraphQL_ComplexityCalculation(t *testing.T) {
	t.Parallel()
	query := "{\n  product {\n    id\n    name\n  }\n}"
	c := graphql.QueryComplexity(query)
	if c < 1 {
		t.Fatalf("expected complexity >= 1, got %d", c)
	}
}

func TestGraphQL_DepthLimitReject(t *testing.T) {
	t.Parallel()
	query := "{ a { b { c { d { e } } } } }"
	if err := graphql.DepthLimit(query, 3); err == nil {
		t.Fatal("expected depth exceeded error")
	}
}

func TestGraphQL_DepthLimitPass(t *testing.T) {
	t.Parallel()
	query := "{ a { b } }"
	if err := graphql.DepthLimit(query, 5); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestGraphQL_UnknownFieldError(t *testing.T) {
	t.Parallel()
	r := graphql.NewResolvers()
	if _, err := r.Resolve("nofield", nil); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestGraphQL_UnknownTypeError(t *testing.T) {
	t.Parallel()
	td := graphql.NewTypeDefs()
	if _, err := td.Fields("Unknown"); err == nil {
		t.Fatal("expected type not found error")
	}
}

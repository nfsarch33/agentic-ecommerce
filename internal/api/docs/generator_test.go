package docs_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/api/docs"
)

func TestDocs_ParseRoutesExtractsMethodAndPath(t *testing.T) {
	t.Parallel()
	handlers := []docs.RouteHandler{
		{Method: "GET", Path: "/products", Description: "List products"},
		{Method: "POST", Path: "/orders", Description: "Create order"},
	}
	result := docs.ParseRoutes(handlers)
	if len(result) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(result))
	}
	if result[0].Method != "GET" {
		t.Fatalf("expected GET, got %s", result[0].Method)
	}
}

func TestDocs_GenerateProducesValidOpenAPI(t *testing.T) {
	t.Parallel()
	routes := []docs.RouteDoc{{Method: "GET", Path: "/products", Description: "List"}}
	spec, err := docs.GenerateOpenAPI(routes)
	if err != nil {
		t.Fatalf("GenerateOpenAPI: %v", err)
	}
	if spec.OpenAPI != "3.0.0" {
		t.Fatalf("expected OpenAPI 3.0.0")
	}
	if _, ok := spec.Paths["/products"]; !ok {
		t.Fatal("expected /products in paths")
	}
}

func TestDocs_ValidateCatchesSchemaMismatch(t *testing.T) {
	t.Parallel()
	routes := []docs.RouteDoc{{
		Method: "POST", Path: "/orders",
		Params: []docs.Param{{Name: "id", In: "body", Required: true, Type: ""}},
	}}
	spec, _ := docs.GenerateOpenAPI(routes)
	errs := docs.ValidateExamples(spec)
	if len(errs) == 0 {
		t.Fatal("expected validation error for missing type")
	}
}

func TestDocs_ValidatePassesValidExamples(t *testing.T) {
	t.Parallel()
	routes := []docs.RouteDoc{{
		Method: "GET", Path: "/products",
		Params: []docs.Param{{Name: "id", In: "query", Required: true, Type: "string"}},
	}}
	spec, _ := docs.GenerateOpenAPI(routes)
	errs := docs.ValidateExamples(spec)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestDocs_EmptyRoutes(t *testing.T) {
	t.Parallel()
	result := docs.ParseRoutes(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d", len(result))
	}
}

func TestDocs_RouteWithQueryParams(t *testing.T) {
	t.Parallel()
	handlers := []docs.RouteHandler{{
		Method: "GET", Path: "/search",
		Params: []docs.Param{{Name: "q", In: "query", Required: false, Type: "string"}},
	}}
	routes := docs.ParseRoutes(handlers)
	if len(routes[0].Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(routes[0].Params))
	}
}

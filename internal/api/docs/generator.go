package docs

import (
	"errors"
	"strings"
)

type RouteHandler struct {
	Method      string
	Path        string
	Description string
	Params      []Param
}

type Param struct {
	Name     string
	In       string // "query", "path", "body"
	Required bool
	Type     string
}

type RouteDoc struct {
	Method      string
	Path        string
	Description string
	Params      []Param
}

type OpenAPISpec struct {
	OpenAPI string
	Paths   map[string]map[string]PathItem
}

type PathItem struct {
	Description string
	Parameters  []Param
}

type ValidationError struct {
	Path    string
	Message string
}

func ParseRoutes(handlers []RouteHandler) []RouteDoc {
	docs := make([]RouteDoc, 0, len(handlers))
	for _, h := range handlers {
		docs = append(docs, RouteDoc{
			Method:      strings.ToUpper(h.Method),
			Path:        h.Path,
			Description: h.Description,
			Params:      h.Params,
		})
	}
	return docs
}

func GenerateOpenAPI(routes []RouteDoc) (OpenAPISpec, error) {
	spec := OpenAPISpec{
		OpenAPI: "3.0.0",
		Paths:   make(map[string]map[string]PathItem),
	}
	for _, r := range routes {
		if spec.Paths[r.Path] == nil {
			spec.Paths[r.Path] = make(map[string]PathItem)
		}
		spec.Paths[r.Path][strings.ToLower(r.Method)] = PathItem{
			Description: r.Description,
			Parameters:  r.Params,
		}
	}
	return spec, nil
}

var ErrSchemaMismatch = errors.New("example does not match schema")

func ValidateExamples(spec OpenAPISpec) []ValidationError {
	var errs []ValidationError
	for path, methods := range spec.Paths {
		for _, item := range methods {
			for _, p := range item.Parameters {
				if p.Required && p.Type == "" {
					errs = append(errs, ValidationError{Path: path, Message: "required param " + p.Name + " missing type"})
				}
			}
		}
	}
	return errs
}

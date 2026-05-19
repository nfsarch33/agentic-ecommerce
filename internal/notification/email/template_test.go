package email_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/notification/email"
)

func TestRender_SimpleSubstitution(t *testing.T) {
	t.Parallel()
	eng := email.NewTemplateEngine()
	eng.RegisterTemplate("welcome", "Hello {{.Name}}, welcome to {{.Brand}}!")

	out, err := eng.Render("welcome", map[string]any{"Name": "Alice", "Brand": "EC"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if out != "Hello Alice, welcome to EC!" {
		t.Fatalf("got: %q", out)
	}
}

func TestRender_MissingVariable_Error(t *testing.T) {
	t.Parallel()
	eng := email.NewTemplateEngine()
	eng.RegisterTemplate("hi", "Hello {{.Name}}")

	_, err := eng.Render("hi", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing variable")
	}
}

func TestRender_HTMLEscaping(t *testing.T) {
	t.Parallel()
	eng := email.NewTemplateEngine()
	eng.RegisterTemplate("xss", "Value: {{.Input}}")

	out, err := eng.Render("xss", map[string]any{"Input": "<script>alert(1)</script>"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, "<script>") {
		t.Fatalf("XSS not escaped: %q", out)
	}
}

func TestRender_UnknownTemplate_Error(t *testing.T) {
	t.Parallel()
	eng := email.NewTemplateEngine()
	_, err := eng.Render("nonexistent", nil)
	if !errors.Is(err, email.ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got %v", err)
	}
}

func TestListTemplates(t *testing.T) {
	t.Parallel()
	eng := email.NewTemplateEngine()
	eng.RegisterTemplate("t1", "body1")
	eng.RegisterTemplate("t2", "body2")

	ids := eng.ListTemplates()
	if len(ids) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(ids))
	}
}

func TestValidateTemplate_Invalid(t *testing.T) {
	t.Parallel()
	eng := email.NewTemplateEngine()
	err := eng.RegisterTemplate("bad", "{{.Unclosed")
	if err == nil {
		t.Fatal("expected parse error for invalid template")
	}
}

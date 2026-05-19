package email

import (
	"bytes"
	"errors"
	"html/template"
	"sync"
)

var ErrTemplateNotFound = errors.New("template not found")

// TemplateEngine manages and renders HTML email templates.
type TemplateEngine struct {
	mu        sync.RWMutex
	templates map[string]*template.Template
}

func NewTemplateEngine() *TemplateEngine {
	return &TemplateEngine{
		templates: make(map[string]*template.Template),
	}
}

// RegisterTemplate parses and stores the template. Returns parse errors immediately.
func (e *TemplateEngine) RegisterTemplate(id, body string) error {
	tmpl, err := template.New(id).Option("missingkey=error").Parse(body)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.templates[id] = tmpl
	return nil
}

// Render executes the named template with the provided data map.
func (e *TemplateEngine) Render(id string, data map[string]any) (string, error) {
	e.mu.RLock()
	tmpl, ok := e.templates[id]
	e.mu.RUnlock()
	if !ok {
		return "", ErrTemplateNotFound
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ListTemplates returns all registered template IDs.
func (e *TemplateEngine) ListTemplates() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ids := make([]string, 0, len(e.templates))
	for id := range e.templates {
		ids = append(ids, id)
	}
	return ids
}

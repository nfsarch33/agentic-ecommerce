// Package emailsvc provides email templating with SMTP and SES-compatible adapter support.
package emailsvc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"sync"
)

// ErrTemplateNotFound is returned when a template name has not been registered.
var ErrTemplateNotFound = errors.New("emailsvc: template not found")

// ErrNoSender is returned when no sender adapter is configured.
var ErrNoSender = errors.New("emailsvc: no sender configured")

// Message represents a rendered email ready for delivery.
type Message struct {
	From    string
	To      []string
	Subject string
	HTML    string
	Text    string
	Headers map[string]string
}

// SendResult carries a provider-assigned message ID and any send error.
type SendResult struct {
	MessageID string
	Error     error
}

// Sender is the interface implemented by SMTP and SES adapters.
type Sender interface {
	Send(ctx context.Context, msg Message) (SendResult, error)
}

// TemplateData is the map passed to template execution.
type TemplateData map[string]interface{}

// Registry holds named HTML email templates.
type Registry struct {
	mu        sync.RWMutex
	templates map[string]*template.Template
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{templates: make(map[string]*template.Template)}
}

// Register compiles and stores a named HTML template.
func (r *Registry) Register(name, htmlSrc string) error {
	t, err := template.New(name).Parse(htmlSrc)
	if err != nil {
		return fmt.Errorf("emailsvc: parse template %q: %w", name, err)
	}
	r.mu.Lock()
	r.templates[name] = t
	r.mu.Unlock()
	return nil
}

// Render executes the named template with data and returns the rendered HTML.
func (r *Registry) Render(name string, data TemplateData) (string, error) {
	r.mu.RLock()
	t, ok := r.templates[name]
	r.mu.RUnlock()
	if !ok {
		return "", ErrTemplateNotFound
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("emailsvc: render %q: %w", name, err)
	}
	return buf.String(), nil
}

// Service orchestrates template rendering and multi-adapter sending.
type Service struct {
	registry *Registry
	primary  Sender
	fallback Sender
}

// NewService returns a Service with the given registry and adapters.
// fallback may be nil if only one adapter is available.
func NewService(registry *Registry, primary, fallback Sender) *Service {
	return &Service{registry: registry, primary: primary, fallback: fallback}
}

// SendTemplate renders the named template and sends the email via the primary adapter,
// falling back to the secondary adapter on error.
func (s *Service) SendTemplate(ctx context.Context, name string, to []string, subject string, data TemplateData) (SendResult, error) {
	if s.primary == nil {
		return SendResult{}, ErrNoSender
	}
	html, err := s.registry.Render(name, data)
	if err != nil {
		return SendResult{}, err
	}
	msg := Message{To: to, Subject: subject, HTML: html}
	result, err := s.primary.Send(ctx, msg)
	if err != nil && s.fallback != nil {
		result, err = s.fallback.Send(ctx, msg)
	}
	return result, err
}

// StubSender captures sent messages for test assertions.
type StubSender struct {
	mu       sync.Mutex
	Messages []Message
	Err      error
	idSeq    int
}

// Send records the message and returns a synthetic ID.
func (s *StubSender) Send(_ context.Context, msg Message) (SendResult, error) {
	if s.Err != nil {
		return SendResult{Error: s.Err}, s.Err
	}
	s.mu.Lock()
	s.idSeq++
	id := fmt.Sprintf("stub-%d", s.idSeq)
	s.Messages = append(s.Messages, msg)
	s.mu.Unlock()
	return SendResult{MessageID: id}, nil
}

// Sent returns a snapshot of all captured messages.
func (s *StubSender) Sent() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, len(s.Messages))
	copy(out, s.Messages)
	return out
}

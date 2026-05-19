package emailsvc_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/emailsvc"
)

func TestRegistry_RegisterAndRender(t *testing.T) {
	t.Parallel()
	reg := emailsvc.NewRegistry()
	if err := reg.Register("welcome", "<h1>Hello {{.Name}}</h1>"); err != nil {
		t.Fatal(err)
	}
	html, err := reg.Render("welcome", emailsvc.TemplateData{"Name": "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Hello Alice") {
		t.Fatalf("rendered HTML missing expected content: %s", html)
	}
}

func TestRegistry_RenderNotFound(t *testing.T) {
	t.Parallel()
	reg := emailsvc.NewRegistry()
	_, err := reg.Render("nonexistent", nil)
	if !errors.Is(err, emailsvc.ErrTemplateNotFound) {
		t.Fatalf("want ErrTemplateNotFound, got %v", err)
	}
}

func TestRegistry_InvalidTemplate(t *testing.T) {
	t.Parallel()
	reg := emailsvc.NewRegistry()
	err := reg.Register("bad", "{{invalid")
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}

func TestService_SendTemplate_Success(t *testing.T) {
	t.Parallel()
	reg := emailsvc.NewRegistry()
	_ = reg.Register("order", "<p>Order {{.ID}} confirmed</p>")

	stub := &emailsvc.StubSender{}
	svc := emailsvc.NewService(reg, stub, nil)

	result, err := svc.SendTemplate(context.Background(), "order",
		[]string{"buyer@example.com"}, "Order Confirmed",
		emailsvc.TemplateData{"ID": "123"})
	if err != nil {
		t.Fatal(err)
	}
	if result.MessageID == "" {
		t.Fatal("expected message ID")
	}
	sent := stub.Sent()
	if len(sent) != 1 {
		t.Fatalf("want 1 sent, got %d", len(sent))
	}
	if !strings.Contains(sent[0].HTML, "Order 123") {
		t.Fatalf("HTML missing expected content: %s", sent[0].HTML)
	}
}

func TestService_SendTemplate_Fallback(t *testing.T) {
	t.Parallel()
	reg := emailsvc.NewRegistry()
	_ = reg.Register("tpl", "<p>Hi</p>")

	primary := &emailsvc.StubSender{Err: errors.New("smtp timeout")}
	fallback := &emailsvc.StubSender{}
	svc := emailsvc.NewService(reg, primary, fallback)

	_, err := svc.SendTemplate(context.Background(), "tpl",
		[]string{"u@example.com"}, "Subject", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fallback.Sent()) != 1 {
		t.Fatal("expected fallback to receive the message")
	}
}

func TestService_SendTemplate_NoSender(t *testing.T) {
	t.Parallel()
	reg := emailsvc.NewRegistry()
	svc := emailsvc.NewService(reg, nil, nil)
	_, err := svc.SendTemplate(context.Background(), "x", nil, "", nil)
	if !errors.Is(err, emailsvc.ErrNoSender) {
		t.Fatalf("want ErrNoSender, got %v", err)
	}
}

func TestService_SendTemplate_BothAdaptersFail(t *testing.T) {
	t.Parallel()
	reg := emailsvc.NewRegistry()
	_ = reg.Register("tpl", "<p>Hi</p>")
	primary := &emailsvc.StubSender{Err: errors.New("primary fail")}
	fallback := &emailsvc.StubSender{Err: errors.New("fallback fail")}
	svc := emailsvc.NewService(reg, primary, fallback)
	_, err := svc.SendTemplate(context.Background(), "tpl", []string{"x"}, "S", nil)
	if err == nil {
		t.Fatal("expected error when both adapters fail")
	}
}

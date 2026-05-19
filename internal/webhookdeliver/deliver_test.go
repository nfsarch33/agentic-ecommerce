package webhookdeliver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/webhookdeliver"
)

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

// --- Sign / Verify ---

func TestSign_Deterministic(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret")
	payload := []byte(`{"event":"order.created"}`)
	s1 := webhookdeliver.Sign(secret, payload)
	s2 := webhookdeliver.Sign(secret, payload)
	if s1 != s2 {
		t.Fatal("Sign must be deterministic")
	}
}

func TestVerify_Valid(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret")
	payload := []byte(`{"event":"order.created"}`)
	sig := "sha256=" + webhookdeliver.Sign(secret, payload)
	if err := webhookdeliver.Verify(secret, sig, payload); err != nil {
		t.Fatal(err)
	}
}

func TestVerify_Tampered(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret")
	payload := []byte(`{"event":"order.created"}`)
	sig := "sha256=aaaa"
	if err := webhookdeliver.Verify(secret, sig, payload); err == nil {
		t.Fatal("expected error for tampered signature")
	}
}

func TestVerify_MissingPrefix(t *testing.T) {
	t.Parallel()
	if err := webhookdeliver.Verify([]byte("s"), "no-prefix", []byte("data")); err == nil {
		t.Fatal("expected error for missing prefix")
	}
}

// --- Dispatcher Enqueue ---

func TestDispatcher_Enqueue(t *testing.T) {
	t.Parallel()
	d := webhookdeliver.NewDispatcher(webhookdeliver.Config{})
	del, err := d.Enqueue("http://example.com", "order.created", map[string]string{"id": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if del.Status != "pending" {
		t.Fatalf("want pending, got %q", del.Status)
	}
}

// --- Process: successful delivery ---

func TestDispatcher_Process_Success(t *testing.T) {
	t.Parallel()
	var received atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := webhookdeliver.NewDispatcher(webhookdeliver.Config{
		Secret:      []byte("sec"),
		HTTPClient:  srv.Client(),
		MaxAttempts: 3,
	})
	del, _ := d.Enqueue(srv.URL, "order.created", map[string]string{"id": "x"})
	_ = d.Process(context.Background())

	if !received.Load() {
		t.Fatal("server never received the delivery")
	}
	if del.Status != "delivered" {
		t.Fatalf("want delivered, got %q", del.Status)
	}
}

// --- Process: DLQ after max attempts ---

func TestDispatcher_Process_DLQAfterMaxAttempts(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	now := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	d := webhookdeliver.NewDispatcher(webhookdeliver.Config{
		Secret:      []byte("sec"),
		HTTPClient:  srv.Client(),
		MaxAttempts: 3,
		BaseDelay:   1 * time.Millisecond,
		Now:         fixedNow(now),
	})
	del, _ := d.Enqueue(srv.URL, "order.failed", map[string]string{"id": "y"})

	// Drive past max attempts by advancing time and calling Process.
	for i := 0; i < 5; i++ {
		now = now.Add(time.Second)
		d.SetClock(fixedNow(now))
		_ = d.Process(context.Background())
	}

	if del.Status != "dlq" {
		t.Fatalf("want dlq, got %q", del.Status)
	}
	entries := d.DLQEntries()
	if len(entries) == 0 {
		t.Fatal("expected at least one DLQ entry")
	}
}

// --- Verify signature on received request ---

func TestDispatcher_SignsRequest(t *testing.T) {
	t.Parallel()
	secret := []byte("webhook-secret")
	var sigErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&buf)
		sigErr = webhookdeliver.Verify(secret, r.Header.Get("X-Webhook-Signature"), buf)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := webhookdeliver.NewDispatcher(webhookdeliver.Config{
		Secret:     secret,
		HTTPClient: srv.Client(),
	})
	_, _ = d.Enqueue(srv.URL, "test.event", map[string]string{"k": "v"})
	_ = d.Process(context.Background())

	if sigErr != nil {
		t.Fatalf("server signature verification failed: %v", sigErr)
	}
}

func TestDispatcher_PendingCount(t *testing.T) {
	t.Parallel()
	d := webhookdeliver.NewDispatcher(webhookdeliver.Config{})
	_, _ = d.Enqueue("http://example.com", "e1", nil)
	_, _ = d.Enqueue("http://example.com", "e2", nil)
	if d.PendingCount() != 2 {
		t.Fatalf("want 2, got %d", d.PendingCount())
	}
}

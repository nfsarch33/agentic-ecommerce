package pkgtrack_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/pkgtrack"
)

// ---------------------------------------------------------------------------
// StatusNormalizer tests
// ---------------------------------------------------------------------------

func TestNormalizeKnownStatus(t *testing.T) {
	t.Parallel()
	n := pkgtrack.NewStatusNormalizer()
	n.Add("shipped", pkgtrack.StatusInTransit)
	n.Add("delivered_ok", pkgtrack.StatusDelivered)

	if got := n.Normalize("shipped"); got != pkgtrack.StatusInTransit {
		t.Fatalf("want %q, got %q", pkgtrack.StatusInTransit, got)
	}
	if got := n.Normalize("delivered_ok"); got != pkgtrack.StatusDelivered {
		t.Fatalf("want %q, got %q", pkgtrack.StatusDelivered, got)
	}
}

func TestNormalizeUnknownStatus(t *testing.T) {
	t.Parallel()
	n := pkgtrack.NewStatusNormalizer()
	if got := n.Normalize("totally_unknown_xyz"); got != pkgtrack.StatusUnknown {
		t.Fatalf("want %q, got %q", pkgtrack.StatusUnknown, got)
	}
}

// ---------------------------------------------------------------------------
// EventStore tests
// ---------------------------------------------------------------------------

func TestEventStoreIngestAndRetrieve(t *testing.T) {
	t.Parallel()
	store := pkgtrack.NewEventStore()

	e1 := pkgtrack.Event{
		ID: "e1", TrackingNo: "TRK-001",
		RawStatus: "shipped", NormalizedStatus: pkgtrack.StatusInTransit,
		Location: "Sydney", OccurredAt: time.Now(),
	}
	e2 := pkgtrack.Event{
		ID: "e2", TrackingNo: "TRK-001",
		RawStatus: "delivered_ok", NormalizedStatus: pkgtrack.StatusDelivered,
		Location: "Melbourne", OccurredAt: time.Now().Add(time.Hour),
	}

	store.Ingest(e1)
	store.Ingest(e2)

	events := store.Events("TRK-001")
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}
	if events[0].ID != "e1" || events[1].ID != "e2" {
		t.Fatalf("unexpected order: %v", events)
	}
}

func TestEventStoreEmptyReturnsNil(t *testing.T) {
	t.Parallel()
	store := pkgtrack.NewEventStore()
	if got := store.Events("NO_SUCH"); got != nil {
		t.Fatalf("want nil, got %v", got)
	}
}

func TestEventStoreLatestEvent(t *testing.T) {
	t.Parallel()
	store := pkgtrack.NewEventStore()

	e1 := pkgtrack.Event{ID: "e1", TrackingNo: "TRK-002", OccurredAt: time.Now()}
	e2 := pkgtrack.Event{ID: "e2", TrackingNo: "TRK-002", OccurredAt: time.Now().Add(time.Hour)}
	store.Ingest(e1)
	store.Ingest(e2)

	latest, err := store.Latest("TRK-002")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest.ID != "e2" {
		t.Fatalf("want latest id e2, got %q", latest.ID)
	}
}

func TestEventStoreLatestNotFound(t *testing.T) {
	t.Parallel()
	store := pkgtrack.NewEventStore()
	_, err := store.Latest("GHOST")
	if err == nil {
		t.Fatal("want error for missing tracking number, got nil")
	}
}

// ---------------------------------------------------------------------------
// WebhookNotifier tests
// ---------------------------------------------------------------------------

// fakeClient records outbound HTTP calls.
type fakeClient struct {
	req *http.Request
	bodyStr string
}

func (f *fakeClient) Do(req *http.Request) (*http.Response, error) {
	f.req = req
	body, _ := io.ReadAll(req.Body)
	f.bodyStr = string(body)
	rec := httptest.NewRecorder()
	rec.WriteHeader(http.StatusOK)
	return rec.Result(), nil
}

func TestWebhookNotifySendsPOST(t *testing.T) {
	t.Parallel()

	notifier := pkgtrack.NewWebhookNotifier()
	notifier.RegisterHook("TRK-003", "http://example.com/hook")

	client := &fakeClient{}
	evt := pkgtrack.Event{
		ID: "evtX", TrackingNo: "TRK-003",
		RawStatus: "out", NormalizedStatus: pkgtrack.StatusOutForDelivery,
		Location: "Brisbane", OccurredAt: time.Now(),
	}
	notifier.Notify(context.Background(), "TRK-003", evt, client)

	if client.req == nil {
		t.Fatal("expected HTTP request to be made, got nil")
	}
	if client.req.Method != http.MethodPost {
		t.Fatalf("want POST, got %s", client.req.Method)
	}
	if !strings.Contains(client.bodyStr, "TRK-003") {
		t.Fatalf("body does not contain tracking number: %q", client.bodyStr)
	}
}

func TestWebhookNotifyNoHooksNoCall(t *testing.T) {
	t.Parallel()
	notifier := pkgtrack.NewWebhookNotifier()
	client := &fakeClient{}
	evt := pkgtrack.Event{ID: "e1", TrackingNo: "TRK-NONE"}
	notifier.Notify(context.Background(), "TRK-NONE", evt, client)
	if client.req != nil {
		t.Fatal("expected no HTTP call when no hooks registered")
	}
}

package main

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/adapter/inmemory"
	"github.com/nfsarch33/helixon-ec/internal/registration"
)

// newTestServer returns a server preconfigured for v2.5.0 unit tests.
// It seeds the in-memory product/order/cart repositories and lets the
// rest of the wiring fall through to the production newServer
// constructor.
func newTestServer(t *testing.T) *server {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	srv := newServer(
		logger,
		inmemory.NewProductRepository(),
		inmemory.NewOrderRepository(),
		inmemory.NewCartRepository(),
	)
	t.Cleanup(func() { srv.Close() })
	return srv
}

// mostRecentToken inspects the registration recorder for the latest
// Submit token. We expose it as a test helper because the registration
// service intentionally does not echo the token back to clients in
// production.
func mostRecentToken(srv *server) (string, bool) {
	if srv.registrationSvc == nil {
		return "", false
	}
	rec, ok := registrationRecorderFromServer(srv).(*registration.Recorder)
	if !ok || rec == nil {
		return "", false
	}
	events := rec.Events()
	if len(events) == 0 {
		return "", false
	}
	return events[0].Token, events[0].Token != ""
}

// registrationRecorderFromServer is a test seam: the production wiring
// uses *registration.Recorder for the notifier so we can read it back.
// The recorder is not exposed on the server struct (production code
// has no business reading the events), so we reach via the Service's
// internal field via a method exported only for tests.
func registrationRecorderFromServer(srv *server) any {
	return srv.registrationNotifier()
}

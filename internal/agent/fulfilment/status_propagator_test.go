package fulfilment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/stretchr/testify/require"
)

type stubChannel struct {
	mu         sync.Mutex
	name       string
	failsLeft  int
	calls      int
	failingErr error
}

func (s *stubChannel) ChannelName() string { return s.name }

func (s *stubChannel) UpdateOrderStatus(_ context.Context, _ ChannelStatusUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.failsLeft > 0 {
		s.failsLeft--
		if s.failingErr != nil {
			return s.failingErr
		}
		return errors.New("transient channel failure")
	}
	return nil
}

func makeShipmentPayload(status, trackingNumber, eventID string) eventbus.ShipmentStatusUpdatedPayload {
	return eventbus.ShipmentStatusUpdatedPayload{
		Version:        eventbus.ShipmentStatusUpdatedPayloadVersion,
		TenantID:       "tenant-a",
		OrderID:        "ord-1",
		Carrier:        "auspost",
		TrackingNumber: trackingNumber,
		Status:         status,
		EventID:        eventID,
		OccurredAt:     time.Unix(1700000000, 0).UTC(),
	}
}

func TestStatusPropagator_UpdatesAllChannelsOnDelivery(t *testing.T) {
	t.Parallel()
	tt := &stubChannel{name: "tiktok"}
	fb := &stubChannel{name: "facebook"}
	wc := &stubChannel{name: "woocommerce"}

	p, err := NewStatusPropagator(nil, StatusPropagatorConfig{
		Channels:  []ChannelStatusUpdater{tt, fb, wc},
		Publisher: &captureBus{},
		Sleep:     func(time.Duration) {}, // no real sleep in tests
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	res, err := p.Propagate(context.Background(), makeShipmentPayload("delivered", "AP-1", "evt-1"))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"tiktok", "facebook", "woocommerce"}, res.Updated)
	require.Empty(t, res.Failed)
	require.Equal(t, 1, tt.calls)
	require.Equal(t, 1, fb.calls)
	require.Equal(t, 1, wc.calls)
}

func TestStatusPropagator_HandlesPartialChannelFailure(t *testing.T) {
	t.Parallel()
	tt := &stubChannel{name: "tiktok"}
	fb := &stubChannel{name: "facebook", failsLeft: 2}
	wc := &stubChannel{name: "woocommerce"}

	p, err := NewStatusPropagator(nil, StatusPropagatorConfig{
		Channels:   []ChannelStatusUpdater{tt, fb, wc},
		Publisher:  &captureBus{},
		MaxRetries: 3,
		Sleep:      func(time.Duration) {},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	res, err := p.Propagate(context.Background(), makeShipmentPayload("delivered", "AP-1", "evt-1"))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"tiktok", "facebook", "woocommerce"}, res.Updated)
	require.Empty(t, res.Failed)
	require.Equal(t, 3, fb.calls, "facebook fails twice then succeeds on attempt 3")
}

func TestStatusPropagator_PersistentChannelFailureSurfaced(t *testing.T) {
	t.Parallel()
	tt := &stubChannel{name: "tiktok"}
	fb := &stubChannel{name: "facebook", failsLeft: 100} // never recovers
	wc := &stubChannel{name: "woocommerce"}

	p, err := NewStatusPropagator(nil, StatusPropagatorConfig{
		Channels:   []ChannelStatusUpdater{tt, fb, wc},
		Publisher:  &captureBus{},
		MaxRetries: 3,
		Sleep:      func(time.Duration) {},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	res, err := p.Propagate(context.Background(), makeShipmentPayload("delivered", "AP-1", "evt-1"))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"tiktok", "woocommerce"}, res.Updated)
	require.ElementsMatch(t, []string{"facebook"}, res.Failed)
}

func TestStatusPropagator_IdempotentRetryReturnsCached(t *testing.T) {
	t.Parallel()
	tt := &stubChannel{name: "tiktok"}
	p, err := NewStatusPropagator(nil, StatusPropagatorConfig{
		Channels:  []ChannelStatusUpdater{tt},
		Publisher: &captureBus{},
		Sleep:     func(time.Duration) {},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	payload := makeShipmentPayload("delivered", "AP-1", "evt-1")
	first, err := p.Propagate(context.Background(), payload)
	require.NoError(t, err)
	require.False(t, first.Cached)

	second, err := p.Propagate(context.Background(), payload)
	require.NoError(t, err)
	require.True(t, second.Cached)
	require.Equal(t, 1, tt.calls, "second call must not re-dispatch")
}

func TestStatusPropagator_60SecondLatencyAcceptance(t *testing.T) {
	t.Parallel()
	channels := []ChannelStatusUpdater{
		&stubChannel{name: "tiktok"},
		&stubChannel{name: "facebook"},
		&stubChannel{name: "woocommerce"},
	}
	clock := time.Unix(1700000000, 0).UTC()
	p, err := NewStatusPropagator(nil, StatusPropagatorConfig{
		Channels:  channels,
		Publisher: &captureBus{},
		Sleep:     func(time.Duration) {},
		Now:       func() time.Time { defer func() { clock = clock.Add(time.Second) }(); return clock },
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	res, err := p.Propagate(context.Background(), makeShipmentPayload("delivered", "AP-1", "evt-1"))
	require.NoError(t, err)
	require.Less(t, res.Duration, DefaultStatusPropagationLatencyBudget, "3-channel propagation must be <60s")
	require.Len(t, res.Updated, 3)
}

// HMAC verify-then-parse webhook flow (AusPost path).
func TestStatusPropagator_AusPostWebhookHMACVerify(t *testing.T) {
	t.Parallel()
	secret := "shared-auspost-secret"
	body := []byte(`{"tracking_number":"AP-1","status":"delivered","event_id":"evt-1","occurred_at":"2026-05-10T00:00:00Z"}`)
	path := "/api/v1/webhooks/auspost/status"

	tt := &stubChannel{name: "tiktok"}
	prop, err := NewStatusPropagator(nil, StatusPropagatorConfig{
		Channels:  []ChannelStatusUpdater{tt},
		Publisher: &captureBus{},
		Sleep:     func(time.Duration) {},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = prop.Close(context.Background()) })

	lookup := NewMemoryOrderLookup(map[string][2]string{"AP-1": {"ord-1", "tenant-a"}})
	handler, err := NewAusPostWebhookHandler(nil, CarrierWebhookConfig{
		Secret:      secret,
		Path:        path,
		Propagator:  prop,
		OrderLookup: lookup,
	})
	require.NoError(t, err)

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	sig := computeHMAC(secret, http.MethodPost, path, body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(body))
	req.Header.Set("X-AusPost-Signature", sig)
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.Equal(t, 1, tt.calls)

	// Tampered signature -> 401.
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(body))
	req2.Header.Set("X-AusPost-Signature", sig+"deadbeef")
	resp2, err := srv.Client().Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp2.StatusCode)
}

// HMAC verify-then-parse webhook flow (DHL path).
func TestStatusPropagator_DHLWebhookHMACVerify(t *testing.T) {
	t.Parallel()
	secret := "shared-dhl-secret"
	body := []byte(`{"tracking_number":"DHL-9","status":"in_transit","event_id":"evt-9","occurred_at":"2026-05-10T00:00:00Z"}`)
	path := "/api/v1/webhooks/dhl/status"

	tt := &stubChannel{name: "tiktok"}
	prop, err := NewStatusPropagator(nil, StatusPropagatorConfig{
		Channels:  []ChannelStatusUpdater{tt},
		Publisher: &captureBus{},
		Sleep:     func(time.Duration) {},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = prop.Close(context.Background()) })

	lookup := NewMemoryOrderLookup(map[string][2]string{"DHL-9": {"ord-9", "tenant-a"}})
	handler, err := NewDHLWebhookHandler(nil, CarrierWebhookConfig{
		Secret:      secret,
		Path:        path,
		Propagator:  prop,
		OrderLookup: lookup,
	})
	require.NoError(t, err)

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	sig := computeHMAC(secret, http.MethodPost, path, body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(body))
	req.Header.Set("X-DHL-Signature", sig)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
}

func TestStatusPropagator_WebhookUnknownTrackingReturns404(t *testing.T) {
	t.Parallel()
	secret := "s"
	body := []byte(`{"tracking_number":"AP-99","status":"delivered","event_id":"evt-99","occurred_at":"2026-05-10T00:00:00Z"}`)
	path := "/api/v1/webhooks/auspost/status"

	prop, err := NewStatusPropagator(nil, StatusPropagatorConfig{
		Channels:  []ChannelStatusUpdater{&stubChannel{name: "tiktok"}},
		Publisher: &captureBus{},
		Sleep:     func(time.Duration) {},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = prop.Close(context.Background()) })

	handler, err := NewAusPostWebhookHandler(nil, CarrierWebhookConfig{
		Secret:      secret,
		Path:        path,
		Propagator:  prop,
		OrderLookup: NewMemoryOrderLookup(nil), // empty
	})
	require.NoError(t, err)

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	sig := computeHMAC(secret, http.MethodPost, path, body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(body))
	req.Header.Set("X-AusPost-Signature", sig)
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestStatusPropagator_ClosedReturnsError(t *testing.T) {
	t.Parallel()
	p, err := NewStatusPropagator(nil, StatusPropagatorConfig{
		Channels:  []ChannelStatusUpdater{&stubChannel{name: "x"}},
		Publisher: &captureBus{},
	})
	require.NoError(t, err)
	require.NoError(t, p.Close(context.Background()))

	_, err = p.Propagate(context.Background(), makeShipmentPayload("delivered", "AP-1", "evt-1"))
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrStatusPropagatorClosed))
}

func computeHMAC(secret, method, path string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(method + "\n" + path + "\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

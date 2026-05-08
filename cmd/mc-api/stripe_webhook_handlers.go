package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/billing"
)

// stripeWebhookMaxBody caps the Stripe request body to a megabyte.
// Stripe events are <= 64KB; the cap defends against zip-bomb / DoS
// while still leaving plenty of headroom.
const stripeWebhookMaxBody int64 = 1 << 20

// stripeWebhookHandler is the public-facing /webhooks/stripe endpoint.
// It is wired without RBAC because Stripe authenticates via the
// signature header, not via JWT/API token. Verification ALWAYS
// precedes JSON parsing so a malformed payload cannot bypass the
// signature gate.
func (s *server) stripeWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if s.stripeWebhookVerifier == nil || s.billingDispatcher == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "stripe_webhook_unconfigured"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	signature := strings.TrimSpace(r.Header.Get("Stripe-Signature"))
	body, err := io.ReadAll(io.LimitReader(r.Body, stripeWebhookMaxBody+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read_body_failed"})
		return
	}
	if int64(len(body)) > stripeWebhookMaxBody {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "body_too_large"})
		return
	}
	if err := s.stripeWebhookVerifier.Verify(signature, body); err != nil {
		s.respondToWebhookVerifyError(w, err)
		return
	}
	eventID, err := s.processStripeWebhook(r.Context(), body)
	if err != nil {
		s.log.Error("stripe webhook process", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "event_id": eventID})
}

func (s *server) processStripeWebhook(ctx context.Context, body []byte) (string, error) {
	var head struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &head); err != nil {
		return "", err
	}
	if head.ID == "" {
		return "", errors.New("stripe event missing id")
	}
	seen, err := s.billingRepo.StripeEventSeen(ctx, head.ID)
	if err != nil {
		return head.ID, err
	}
	if seen {
		return head.ID, nil
	}
	if _, err := s.billingDispatcher.Dispatch(ctx, body); err != nil {
		return head.ID, err
	}
	if err := s.billingRepo.StripeEventRecord(ctx, head.ID, head.Type, time.Now()); err != nil {
		return head.ID, err
	}
	return head.ID, nil
}

func (s *server) respondToWebhookVerifyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, billing.ErrMissingSignature),
		errors.Is(err, billing.ErrSignatureMalformed):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "signature_invalid"})
	case errors.Is(err, billing.ErrSignatureMismatch),
		errors.Is(err, billing.ErrEventTooOld):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "signature_invalid"})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "signature_invalid"})
	}
}

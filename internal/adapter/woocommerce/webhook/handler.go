package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	enginesync "github.com/nfsarch33/agentic-ecommerce/internal/sync"
)

const maxPayloadSize = 1 * 1024 * 1024

const (
	ResourceOrders   = "orders"
	ResourceProducts = "products"
)

type EventRecorder interface {
	RecordEvent(enginesync.Event)
}

type Config struct {
	Secret   string
	Resource string
	Recorder EventRecorder
}

type Handler struct {
	secret   string
	resource string
	recorder EventRecorder
}

type orderPayload struct {
	ID       int    `json:"id"`
	Status   string `json:"status"`
	Total    string `json:"total"`
	Currency string `json:"currency,omitempty"`
	Billing  struct {
		Email string `json:"email"`
	} `json:"billing"`
	LineItems []struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		ProductID int    `json:"product_id"`
		Quantity  int    `json:"quantity"`
		Total     string `json:"total"`
	} `json:"line_items"`
}

type productPayload struct {
	ID   int    `json:"id"`
	SKU  string `json:"sku"`
	Name string `json:"name"`
}

func NewHandler(cfg Config) *Handler {
	return &Handler{
		secret:   cfg.Secret,
		resource: cfg.Resource,
		recorder: cfg.Recorder,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPayloadSize+1))
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "read_body_failed"})
		return
	}
	if len(body) > maxPayloadSize {
		respondJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "payload_too_large"})
		return
	}
	if len(body) == 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "empty_body"})
		return
	}
	if !VerifySignature(body, r.Header.Get("X-WC-Webhook-Signature"), h.secret) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_signature"})
		return
	}

	switch h.resource {
	case ResourceOrders:
		h.handleOrder(w, r, body)
	case ResourceProducts:
		h.handleProduct(w, r, body)
	default:
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid_webhook_resource"})
	}
}

func VerifySignature(body []byte, signature, secret string) bool {
	signature = strings.TrimSpace(signature)
	if signature == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	raw := mac.Sum(nil)
	base64Expected := base64.StdEncoding.EncodeToString(raw)
	hexExpected := hex.EncodeToString(raw)
	return hmac.Equal([]byte(base64Expected), []byte(signature)) || hmac.Equal([]byte(hexExpected), []byte(signature))
}

func (h *Handler) handleOrder(w http.ResponseWriter, r *http.Request, body []byte) {
	var payload orderPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	if payload.ID <= 0 || strings.TrimSpace(payload.Status) == "" {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_order_payload"})
		return
	}
	topic := r.Header.Get("X-WC-Webhook-Topic")
	if topic == "" {
		topic = "order.updated"
	}
	h.record(enginesync.Event{
		Type:     enginesync.EventInventoryReconciled,
		RemoteID: payload.ID,
		Message:  "woocommerce order webhook accepted",
		Metadata: map[string]string{
			"topic":  topic,
			"status": payload.Status,
			"total":  payload.Total,
		},
	})
	respondJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (h *Handler) handleProduct(w http.ResponseWriter, r *http.Request, body []byte) {
	var payload productPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	if payload.ID <= 0 || strings.TrimSpace(payload.SKU) == "" || strings.TrimSpace(payload.Name) == "" {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_product_payload"})
		return
	}
	topic := r.Header.Get("X-WC-Webhook-Topic")
	if topic == "" {
		topic = "product.updated"
	}
	h.record(enginesync.Event{
		Type:     enginesync.EventProductImported,
		RemoteID: payload.ID,
		Message:  "woocommerce product webhook accepted",
		Metadata: map[string]string{
			"topic": topic,
			"sku":   payload.SKU,
			"name":  payload.Name,
		},
	})
	respondJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (h *Handler) record(event enginesync.Event) {
	if h.recorder != nil {
		h.recorder.RecordEvent(event)
	}
}

func respondJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (p productPayload) String() string {
	return fmt.Sprintf("%d:%s", p.ID, p.SKU)
}

package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

type moneyResponse struct {
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
}

type productResponse struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Slug        string        `json:"slug"`
	Price       moneyResponse `json:"price"`
	Stock       int           `json:"stock"`
	Description string        `json:"description,omitempty"`
}

type serverConfig struct {
	allowedOrigin string
	apiToken      string
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	addr := getenv("ECOMMERCE_HTTP_ADDR", "127.0.0.1:8080")
	logger.Info("mc-api.start", "addr", addr)
	if err := http.ListenAndServe(addr, newMux()); err != nil {
		logger.Error("mc-api.stop", "error", err)
		os.Exit(1)
	}
}

func newMux() *http.ServeMux {
	cfg := serverConfig{
		allowedOrigin: getenv("ECOMMERCE_ALLOWED_ORIGIN", ""),
		apiToken:      getenv("ECOMMERCE_API_TOKEN", ""),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/api/v1/products", withCORS(cfg, withBearerAuth(cfg, productsHandler)))
	return mux
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "agentic-ecommerce-mc-api",
	})
}

func productsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	writeJSON(w, http.StatusOK, seedProducts())
}

func withCORS(cfg serverConfig, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if cfg.allowedOrigin == "" || origin != cfg.allowedOrigin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin_not_allowed"})
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func withBearerAuth(cfg serverConfig, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.apiToken == "" {
			next(w, r)
			return
		}
		got := strings.TrimSpace(r.Header.Get("Authorization"))
		want := "Bearer " + cfg.apiToken
		if got != want {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorised"})
			return
		}
		next(w, r)
	}
}

func seedProducts() []productResponse {
	return []productResponse{
		{
			ID:          "p_resistance-band-set",
			Title:       "Resistance band set",
			Slug:        "resistance-band-set",
			Price:       moneyResponse{Amount: 4995, Currency: "AUD"},
			Stock:       12,
			Description: "Starter strength kit for home training.",
		},
		{
			ID:          "p_foam-roller",
			Title:       "Foam roller",
			Slug:        "foam-roller",
			Price:       moneyResponse{Amount: 3500, Currency: "AUD"},
			Stock:       5,
			Description: "Dense recovery roller for mobility work.",
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

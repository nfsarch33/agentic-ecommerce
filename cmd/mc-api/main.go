package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

type moneyResponse struct {
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
}

type productResponse struct {
	ID          string        `json:"id"`
	SKU         string        `json:"sku"`
	Title       string        `json:"title"`
	Slug        string        `json:"slug"`
	Price       moneyResponse `json:"price"`
	Stock       int           `json:"stock"`
	Status      string        `json:"status"`
	Description string        `json:"description,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type createProductRequest struct {
	SKU         string        `json:"sku"`
	Title       string        `json:"title"`
	Slug        string        `json:"slug,omitempty"`
	Description string        `json:"description,omitempty"`
	Price       moneyResponse `json:"price"`
	Stock       int           `json:"stock"`
	Status      string        `json:"status,omitempty"`
}

type listResponse struct {
	Products []productResponse `json:"products"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PerPage  int               `json:"per_page"`
}

type serverConfig struct {
	allowedOrigin string
	apiToken      string
}

type server struct {
	cfg  serverConfig
	repo port.ProductRepository
	log  *slog.Logger
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	repo := inmemory.NewProductRepository()
	seedDefaultProducts(repo)

	addr := getenv("ECOMMERCE_HTTP_ADDR", "127.0.0.1:8080")
	logger.Info("mc-api.start", "addr", addr)

	srv := newServer(logger, repo)
	if err := http.ListenAndServe(addr, srv.mux()); err != nil {
		logger.Error("mc-api.stop", "error", err)
		os.Exit(1)
	}
}

func newServer(logger *slog.Logger, repo port.ProductRepository) *server {
	return &server{
		cfg: serverConfig{
			allowedOrigin: getenv("ECOMMERCE_ALLOWED_ORIGIN", ""),
			apiToken:      getenv("ECOMMERCE_API_TOKEN", ""),
		},
		repo: repo,
		log:  logger,
	}
}

func (s *server) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)

	api := s.withCORS(s.withBearerAuth(s.productsHandler))
	mux.HandleFunc("/api/v1/products", api)
	mux.HandleFunc("/api/v1/products/", api)

	return mux
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "agentic-ecommerce-mc-api",
	})
}

func (s *server) productsHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/products")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "" && r.Method == http.MethodGet:
		s.listProducts(w, r)
	case path == "" && r.Method == http.MethodPost:
		s.createProduct(w, r)
	case path != "" && r.Method == http.MethodGet:
		s.getProduct(w, r, path)
	case path != "" && r.Method == http.MethodPut:
		s.updateProduct(w, r, path)
	case path != "" && r.Method == http.MethodDelete:
		s.deleteProduct(w, r, path)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) listProducts(w http.ResponseWriter, r *http.Request) {
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	result, err := s.repo.List(r.Context(), page, perPage)
	if err != nil {
		s.log.Error("list products", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	products := make([]productResponse, len(result.Products))
	for i, p := range result.Products {
		products[i] = toProductResponse(p)
	}

	writeJSON(w, http.StatusOK, listResponse{
		Products: products,
		Total:    result.Total,
		Page:     page,
		PerPage:  perPage,
	})
}

func (s *server) createProduct(w http.ResponseWriter, r *http.Request) {
	var req createProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	price, err := catalog.NewMoney(req.Price.Amount, req.Price.Currency)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	status := catalog.StatusDraft
	if req.Status != "" {
		status, err = catalog.ParseProductStatus(req.Status)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
	}

	product, err := catalog.NewProduct(catalog.ProductInput{
		SKU:         req.SKU,
		Title:       req.Title,
		Slug:        req.Slug,
		Description: req.Description,
		Price:       price,
		Stock:       req.Stock,
		Status:      status,
	})
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	if err := s.repo.Create(r.Context(), product); err != nil {
		s.log.Error("create product", "error", err)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "duplicate_product"})
		return
	}

	writeJSON(w, http.StatusCreated, toProductResponse(product))
}

func (s *server) getProduct(w http.ResponseWriter, r *http.Request, idOrSlug string) {
	var (
		product catalog.Product
		err     error
	)

	if id, parseErr := uuid.Parse(idOrSlug); parseErr == nil {
		product, err = s.repo.GetByID(r.Context(), id)
	} else {
		product, err = s.repo.GetBySlug(r.Context(), idOrSlug)
	}

	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get product", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	writeJSON(w, http.StatusOK, toProductResponse(product))
}

func (s *server) updateProduct(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}

	existing, err := s.repo.GetByID(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get product for update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	var req createProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	price, err := catalog.NewMoney(req.Price.Amount, req.Price.Currency)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	status := existing.Status()
	if req.Status != "" {
		status, err = catalog.ParseProductStatus(req.Status)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
	}

	slug := req.Slug
	if slug == "" {
		slug = existing.Slug()
	}

	updated := catalog.ReconstructProduct(catalog.ProductRecord{
		ID:          id,
		SKU:         req.SKU,
		Title:       req.Title,
		Slug:        slug,
		Description: req.Description,
		Price:       price,
		Stock:       req.Stock,
		Status:      status,
		CreatedAt:   existing.CreatedAt(),
		UpdatedAt:   existing.UpdatedAt(),
	})

	if err := s.repo.Update(r.Context(), updated); err != nil {
		s.log.Error("update product", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	writeJSON(w, http.StatusOK, toProductResponse(updated))
}

func (s *server) deleteProduct(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}

	if err := s.repo.Delete(r.Context(), id); err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("delete product", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *server) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if s.cfg.allowedOrigin == "" || origin != s.cfg.allowedOrigin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin_not_allowed"})
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (s *server) withBearerAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.apiToken == "" {
			next(w, r)
			return
		}
		got := strings.TrimSpace(r.Header.Get("Authorization"))
		want := "Bearer " + s.cfg.apiToken
		if got != want {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorised"})
			return
		}
		next(w, r)
	}
}

func toProductResponse(p catalog.Product) productResponse {
	return productResponse{
		ID:          p.ID().String(),
		SKU:         p.SKU(),
		Title:       p.Title(),
		Slug:        p.Slug(),
		Price:       moneyResponse{Amount: p.Price().Amount(), Currency: p.Price().Currency()},
		Stock:       p.Stock(),
		Status:      p.Status().String(),
		Description: p.Description(),
		CreatedAt:   p.CreatedAt(),
		UpdatedAt:   p.UpdatedAt(),
	}
}

func seedDefaultProducts(repo *inmemory.ProductRepository) {
	price1, _ := catalog.NewMoney(4995, "AUD")
	p1, _ := catalog.NewProduct(catalog.ProductInput{
		SKU:         "RESISTANCE-BAND-SET",
		Title:       "Resistance band set",
		Slug:        "resistance-band-set",
		Description: "Starter strength kit for home training.",
		Price:       price1,
		Stock:       12,
		Status:      catalog.StatusActive,
	})
	_ = repo.Create(nil, p1)

	price2, _ := catalog.NewMoney(3500, "AUD")
	p2, _ := catalog.NewProduct(catalog.ProductInput{
		SKU:         "FOAM-ROLLER",
		Title:       "Foam roller",
		Slug:        "foam-roller",
		Description: "Dense recovery roller for mobility work.",
		Price:       price2,
		Stock:       5,
		Status:      catalog.StatusActive,
	})
	_ = repo.Create(nil, p2)
}

func isNotFound(err error) bool {
	return errors.Is(err, inmemory.ErrProductNotFound)
}

func queryInt(r *http.Request, key string, fallback int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
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

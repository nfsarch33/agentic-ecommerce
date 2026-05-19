package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	contentagent "github.com/nfsarch33/helixon-ec/internal/agent/content"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	"github.com/nfsarch33/helixon-ec/internal/rag"
)

func TestRAGSearchAddsHandlerDeadlineBeforeEmbedding(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	srv.rag = rag.NewService(deadlineRequiredEmbedder{}, rag.NewInMemoryVectorStore(1), rag.ChunkOptions{MaxWords: 12})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rag/search?q=resistance", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var payload ragSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Query != "resistance" {
		t.Fatalf("query = %q, want resistance", payload.Query)
	}
}

func TestRAGSearchMapsDeadlineExceededToGatewayTimeout(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	srv.rag = rag.NewService(contextDeadlineEmbedder{}, rag.NewInMemoryVectorStore(1), rag.ChunkOptions{MaxWords: 12})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rag/search?q=resistance", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorResponse(t, rec.Body.Bytes(), "dependency_timeout")
}

func TestGenerateDescriptionUsesScopedProductLookup(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addTenantProduct(t, repo, "tenant-b", "BAND-SCOPE", "Scoped Product", 4995)
	srv.contentAgent = &fakeContentAgent{result: contentagent.GenerateResult{
		GeneratedContent: contentagent.GeneratedContent{
			Description: "Should never be generated for the wrong tenant.",
			SEOTitle:    "Wrong tenant",
		},
		Evaluation: contentagent.Evaluation{Score: 90, Pass: true},
		TokensUsed: 12,
	}}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/products/"+product.ID().String()+"/generate-description",
		bytes.NewBufferString(`{}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorResponse(t, rec.Body.Bytes(), "not_found")
}

func TestGenerateContentWithFactCheckingUsesTenantScopedEvidence(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addTenantProduct(t, repo, "tenant-a", "BAND-EVIDENCE", "Scoped Evidence Product", 4995)
	srv.contentAgent = &fakeContentAgent{result: contentagent.GenerateResult{
		GeneratedContent: contentagent.GeneratedContent{
			Description: "Resistance Band Set includes five resistance levels.",
			SEOTitle:    "Resistance Band Set",
		},
		Evaluation: contentagent.Evaluation{Score: 90, Pass: true},
		TokensUsed: 42,
	}}
	srv.rag = rag.NewService(staticRAGEmbedder{}, rag.NewInMemoryVectorStore(2), rag.ChunkOptions{MaxWords: 20})
	for _, doc := range []rag.Document{
		{
			ID:       "tenant-b-doc",
			TenantID: "tenant-b",
			Title:    "Tenant B Supplier Spec",
			Source:   "supplier-spec",
			Content:  "Resistance Band Set includes five resistance levels for progressive workouts.",
		},
	} {
		if _, err := srv.rag.Ingest(context.Background(), doc); err != nil {
			t.Fatalf("ingest %s: %v", doc.ID, err)
		}
	}
	srv.factChecker = contentagent.NewFactChecker(srv.rag, contentagent.FactCheckOptions{MinConfidence: 0.6, TopK: 3})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/content/generate",
		bytes.NewBufferString(`{"product_id":"`+product.ID().String()+`"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var payload contentFactCheckResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.FactCheck.Pass {
		t.Fatalf("fact-check unexpectedly passed with cross-tenant evidence: %+v", payload.FactCheck)
	}
	for _, claim := range payload.FactCheck.Claims {
		for _, evidence := range claim.Evidence {
			if evidence.TenantID != "" && evidence.TenantID != "tenant-a" {
				t.Fatalf("cross-tenant evidence leaked into fact-check result: %+v", evidence)
			}
		}
	}
}

func TestGenerateContentWithFactCheckingMapsDeadlineExceededToGatewayTimeout(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "BAND-TIMEOUT", "Timeout Product", 4995)
	srv.contentAgent = deadlineAwareContentAgent{}
	srv.factChecker = contentagent.NewFactChecker(rag.NewService(rag.NewHashEmbedder(4), rag.NewInMemoryVectorStore(4), rag.ChunkOptions{}), contentagent.FactCheckOptions{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/content/generate",
		bytes.NewBufferString(`{"product_id":"`+product.ID().String()+`"}`),
	).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504; body=%s", rec.Code, rec.Body.String())
	}
	assertErrorResponse(t, rec.Body.Bytes(), "dependency_timeout")
}

type deadlineRequiredEmbedder struct{}

func (deadlineRequiredEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("missing deadline")
	}
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = []float64{1}
	}
	return out, nil
}

type contextDeadlineEmbedder struct{}

func (contextDeadlineEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(20 * time.Millisecond):
		out := make([][]float64, len(texts))
		for i := range texts {
			out[i] = []float64{1}
		}
		return out, nil
	}
}

type deadlineAwareContentAgent struct{}

func (deadlineAwareContentAgent) Generate(ctx context.Context, _ contentagent.GenerateRequest) (contentagent.GenerateResult, error) {
	select {
	case <-ctx.Done():
		return contentagent.GenerateResult{}, ctx.Err()
	case <-time.After(20 * time.Millisecond):
		return contentagent.GenerateResult{}, nil
	}
}

func addTenantProduct(
	t *testing.T,
	repo interface {
		CreateWithTenant(context.Context, catalog.Product, string) error
	},
	tenantID string,
	sku string,
	title string,
	amount int,
) catalog.Product {
	t.Helper()
	price, err := catalog.NewMoney(amount, "AUD")
	if err != nil {
		t.Fatalf("create money: %v", err)
	}
	product, err := catalog.NewProduct(catalog.ProductInput{
		SKU:    sku,
		Title:  title,
		Price:  price,
		Stock:  10,
		Status: catalog.StatusActive,
	})
	if err != nil {
		t.Fatalf("new product: %v", err)
	}
	if err := repo.CreateWithTenant(context.Background(), product, tenantID); err != nil {
		t.Fatalf("create tenant product: %v", err)
	}
	return product
}

func assertErrorResponse(t *testing.T, body []byte, want string) {
	t.Helper()
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload["error"] != want {
		t.Fatalf("error = %q, want %q; body=%s", payload["error"], want, string(body))
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	"github.com/nfsarch33/agentic-ecommerce/internal/rag"
)

func TestGenerateContentWithFactCheckingReturnsAndStoresResult(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "RB-FACT", "Resistance Band Set", 4995)
	srv.contentAgent = &fakeContentAgent{result: content.GenerateResult{
		GeneratedContent: content.GeneratedContent{
			Description:     "Resistance Band Set includes five resistance levels.",
			SEOTitle:        "Resistance Band Set",
			MetaDescription: "Resistance Band Set includes five resistance levels.",
		},
		Evaluation: content.Evaluation{Score: 90, Pass: true},
		TokensUsed: 42,
	}}
	srv.rag = rag.NewService(rag.NewHashEmbedder(16), rag.NewInMemoryVectorStore(16), rag.ChunkOptions{MaxWords: 24})
	_, err := srv.rag.Ingest(context.Background(), rag.Document{
		ID:      "doc-rb",
		Title:   "Resistance Band Spec",
		Source:  "supplier-spec",
		Content: "Resistance Band Set includes five resistance levels for progressive workouts.",
	})
	if err != nil {
		t.Fatalf("seed rag: %v", err)
	}
	srv.factChecker = content.NewFactChecker(srv.rag, content.FactCheckOptions{MinConfidence: 0.6, TopK: 3})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/content/generate", bytes.NewBufferString(`{"product_id":"`+product.ID().String()+`","max_words":80}`))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var generated contentFactCheckResponse
	if err := json.NewDecoder(rec.Body).Decode(&generated); err != nil {
		t.Fatalf("decode generate: %v", err)
	}
	if generated.FactCheckID == "" || !generated.FactCheck.Pass || !generated.Pass {
		t.Fatalf("generated response = %+v", generated)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/content/fact-checks/"+generated.FactCheckID, nil)
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var stored content.FactCheckResult
	if err := json.NewDecoder(rec.Body).Decode(&stored); err != nil {
		t.Fatalf("decode stored: %v", err)
	}
	if stored.ID != generated.FactCheckID || !stored.Pass {
		t.Fatalf("stored result = %+v", stored)
	}
}

func TestGenerateContentWithFactCheckingRequiresDependencies(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "RB-NODEPS", "Resistance Band Set", 4995)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content/generate", bytes.NewBufferString(`{"product_id":"`+product.ID().String()+`"}`))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGenerateContentWithFactCheckingValidatesRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "invalid json", body: `{`, want: http.StatusBadRequest},
		{name: "invalid product id", body: `{"product_id":"bad"}`, want: http.StatusBadRequest},
		{name: "missing product", body: `{"product_id":"11111111-1111-1111-1111-111111111111"}`, want: http.StatusNotFound},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := testServer(t)
			srv.contentAgent = &fakeContentAgent{}
			srv.factChecker = content.NewFactChecker(rag.NewService(rag.NewHashEmbedder(16), rag.NewInMemoryVectorStore(16), rag.ChunkOptions{}), content.FactCheckOptions{})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/content/generate", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()
			srv.mux().ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestFactCheckLookupHandlesMissingResult(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/content/fact-checks/missing", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGenerateContentWithFactCheckingMapsGenerationFailure(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "RB-GEN-FAIL", "Resistance Band Set", 4995)
	srv.contentAgent = &fakeContentAgent{err: errors.New("bridge unavailable")}
	srv.factChecker = content.NewFactChecker(rag.NewService(rag.NewHashEmbedder(16), rag.NewInMemoryVectorStore(16), rag.ChunkOptions{}), content.FactCheckOptions{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/content/generate", bytes.NewBufferString(`{"product_id":"`+product.ID().String()+`"}`))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGenerateContentWithFactCheckingMapsFactCheckFailure(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "RB-FC-FAIL", "Resistance Band Set", 4995)
	srv.contentAgent = &fakeContentAgent{result: content.GenerateResult{
		GeneratedContent: content.GeneratedContent{Description: "Resistance Band Set includes five resistance levels."},
		Evaluation:       content.Evaluation{Score: 90, Pass: true},
	}}
	srv.factChecker = content.NewFactChecker(errorEvidenceSearcher{}, content.FactCheckOptions{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/content/generate", bytes.NewBufferString(`{"product_id":"`+product.ID().String()+`"}`))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
}

type errorEvidenceSearcher struct{}

func (errorEvidenceSearcher) SearchText(context.Context, string, int) ([]rag.SearchResult, error) {
	return nil, errors.New("rag unavailable")
}

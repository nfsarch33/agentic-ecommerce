package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/rag"
)

func TestRAGDocumentIngestAndEvidenceSearch(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	srv.rag = rag.NewService(rag.NewHashEmbedder(16), rag.NewInMemoryVectorStore(16), rag.ChunkOptions{MaxWords: 24})

	body := `{"id":"doc-rb","title":"Resistance Band Spec","source":"supplier-spec","content":"Resistance Band Set includes five resistance levels and natural latex construction.","metadata":{"sku":"RB-SET"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/documents", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("ingest status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var ingestResp ragIngestResponse
	if err := json.NewDecoder(rec.Body).Decode(&ingestResp); err != nil {
		t.Fatalf("decode ingest: %v", err)
	}
	if ingestResp.DocumentID != "doc-rb" || ingestResp.Chunks == 0 {
		t.Fatalf("ingest response = %+v", ingestResp)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/rag/search?q=five%20resistance%20levels&top_k=3", nil)
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var searchResp ragSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&searchResp); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(searchResp.Results) == 0 || searchResp.Results[0].DocumentID != "doc-rb" {
		t.Fatalf("search response = %+v", searchResp)
	}
}

func TestRAGSearchRequiresConfiguredService(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rag/search?q=test", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRAGHandlersValidateRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "ingest invalid json", method: http.MethodPost, path: "/api/v1/rag/documents", body: `{`, want: http.StatusBadRequest},
		{name: "ingest empty document", method: http.MethodPost, path: "/api/v1/rag/documents", body: `{"id":"empty","content":" "}`, want: http.StatusUnprocessableEntity},
		{name: "search missing query", method: http.MethodGet, path: "/api/v1/rag/search", want: http.StatusBadRequest},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := testServer(t)
			srv.rag = rag.NewService(rag.NewHashEmbedder(16), rag.NewInMemoryVectorStore(16), rag.ChunkOptions{MaxWords: 24})
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()
			srv.mux().ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

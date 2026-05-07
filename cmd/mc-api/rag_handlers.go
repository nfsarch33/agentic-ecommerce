package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nfsarch33/agentic-ecommerce/internal/rag"
)

type ragIngestResponse struct {
	DocumentID string `json:"document_id"`
	Chunks     int    `json:"chunks"`
}

type ragSearchResponse struct {
	Query   string             `json:"query"`
	Results []rag.SearchResult `json:"results"`
}

func (s *server) ragHandler(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/v1/rag/documents" && r.Method == http.MethodPost:
		s.ingestRAGDocument(w, r)
	case r.URL.Path == "/api/v1/rag/search" && r.Method == http.MethodGet:
		s.searchRAGEvidence(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) ingestRAGDocument(w http.ResponseWriter, r *http.Request) {
	if s.rag == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "rag_not_configured"})
		return
	}
	var doc rag.Document
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	result, err := s.rag.Ingest(r.Context(), doc)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, ragIngestResponse{DocumentID: result.Document.ID, Chunks: len(result.Chunks)})
}

func (s *server) searchRAGEvidence(w http.ResponseWriter, r *http.Request) {
	if s.rag == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "rag_not_configured"})
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_query"})
		return
	}
	topK := queryInt(r, "top_k", 5)
	if topK <= 0 || topK > 20 {
		topK = 5
	}
	results, err := s.rag.SearchText(r.Context(), query, topK)
	if err != nil {
		s.log.Error("rag search", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "rag_search_failed"})
		return
	}
	writeJSON(w, http.StatusOK, ragSearchResponse{Query: query, Results: results})
}

package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	contentagent "github.com/nfsarch33/helixon-ec/internal/agent/content"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
)

type generateDescriptionRequest struct {
	Style    string   `json:"style,omitempty"`
	Language string   `json:"language,omitempty"`
	MaxWords int      `json:"max_words,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
}

type contentSuggestionResponse struct {
	ProductID       string                  `json:"product_id"`
	Description     string                  `json:"description"`
	SEOTitle        string                  `json:"seo_title"`
	MetaDescription string                  `json:"meta_description"`
	Score           int                     `json:"score"`
	Pass            bool                    `json:"pass"`
	TokensUsed      int                     `json:"tokens_used"`
	Evaluation      contentagent.Evaluation `json:"evaluation"`
}

func (s *server) generateDescription(w http.ResponseWriter, r *http.Request, path string) {
	var req generateDescriptionRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				s.runContentAgent(w, r, contentProductID(path, "/generate-description"), req)
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
	}
	s.runContentAgent(w, r, contentProductID(path, "/generate-description"), req)
}

func (s *server) aiSuggestions(w http.ResponseWriter, r *http.Request, path string) {
	s.runContentAgent(w, r, contentProductID(path, "/ai-suggestions"), generateDescriptionRequest{})
}

func (s *server) runContentAgent(w http.ResponseWriter, r *http.Request, idPart string, req generateDescriptionRequest) {
	if s.contentAgent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "content_agent_not_configured"})
		return
	}
	r, cancel := s.withDependencyDeadline(r)
	defer cancel()
	id, err := uuid.Parse(idPart)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	product, err := s.productForRequest(r, id.String())
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get product for content agent", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	agentReq := contentagent.GenerateRequest{
		Product:  toContentProductInfo(product),
		Style:    normalizeContentStyle(req.Style),
		Language: req.Language,
		MaxWords: req.MaxWords,
		Keywords: req.Keywords,
	}
	if agentReq.MaxWords == 0 {
		agentReq.MaxWords = 120
	}
	result, err := s.contentAgent.Generate(r.Context(), agentReq)
	if err != nil {
		s.log.Error("generate product content", "product_id", id.String(), "error", err)
		if isDependencyTimeout(err) {
			writeDependencyTimeout(w)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "content_generation_failed"})
		return
	}
	evaluation := normalizeContentEvaluation(result.Evaluation)

	writeJSON(w, http.StatusOK, contentSuggestionResponse{
		ProductID:       id.String(),
		Description:     result.Description,
		SEOTitle:        result.SEOTitle,
		MetaDescription: result.MetaDescription,
		Score:           evaluation.Score,
		Pass:            evaluation.Pass,
		TokensUsed:      result.TokensUsed,
		Evaluation:      evaluation,
	})
}

func contentProductID(path, suffix string) string {
	id := strings.TrimSuffix(path, suffix)
	return strings.Trim(id, "/")
}

func normalizeContentStyle(style string) contentagent.Style {
	switch contentagent.Style(strings.TrimSpace(style)) {
	case contentagent.StyleCasual:
		return contentagent.StyleCasual
	case contentagent.StyleLuxury:
		return contentagent.StyleLuxury
	case contentagent.StyleTechnical:
		return contentagent.StyleTechnical
	default:
		return contentagent.StyleProfessional
	}
}

func toContentProductInfo(product catalog.Product) contentagent.ProductInfo {
	categories := product.Categories()
	names := make([]string, 0, len(categories))
	for _, category := range categories {
		if strings.TrimSpace(category.Name) != "" {
			names = append(names, category.Name)
		}
	}
	return contentagent.ProductInfo{
		ID:          product.ID().String(),
		SKU:         product.SKU(),
		Title:       product.Title(),
		Description: product.Description(),
		PriceAmount: product.Price().Amount(),
		Currency:    product.Price().Currency(),
		Stock:       product.Stock(),
		Categories:  names,
	}
}

func normalizeContentEvaluation(e contentagent.Evaluation) contentagent.Evaluation {
	if e.KeywordDensity == nil {
		e.KeywordDensity = map[string]float64{}
	}
	if e.Tone.Issues == nil {
		e.Tone.Issues = []string{}
	}
	if e.FactualIssues == nil {
		e.FactualIssues = []string{}
	}
	return e
}

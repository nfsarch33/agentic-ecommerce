package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
)

type generateFactCheckedContentRequest struct {
	ProductID string   `json:"product_id"`
	Style     string   `json:"style,omitempty"`
	Language  string   `json:"language,omitempty"`
	MaxWords  int      `json:"max_words,omitempty"`
	Keywords  []string `json:"keywords,omitempty"`
}

type contentFactCheckResponse struct {
	FactCheckID     string                       `json:"fact_check_id"`
	ProductID       string                       `json:"product_id"`
	Description     string                       `json:"description"`
	SEOTitle        string                       `json:"seo_title"`
	MetaDescription string                       `json:"meta_description"`
	Score           int                          `json:"score"`
	Pass            bool                         `json:"pass"`
	TokensUsed      int                          `json:"tokens_used"`
	Evaluation      contentagent.Evaluation      `json:"evaluation"`
	FactCheck       contentagent.FactCheckResult `json:"fact_check"`
}

type storedFactCheckResult struct {
	result   contentagent.FactCheckResult
	tenantID string
}

func (s *server) contentFactCheckHandler(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/v1/content/generate" && r.Method == http.MethodPost:
		s.generateContentWithFactCheck(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/v1/content/fact-checks/") && r.Method == http.MethodGet:
		s.getFactCheckResult(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) generateContentWithFactCheck(w http.ResponseWriter, r *http.Request) {
	if s.contentAgent == nil || s.factChecker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "content_fact_check_not_configured"})
		return
	}
	r, cancel := s.withDependencyDeadline(r)
	defer cancel()
	var req generateFactCheckedContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_product_id"})
		return
	}
	product, err := s.productForRequest(r, productID.String())
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get product for content fact check", "error", err)
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
		s.log.Error("generate fact checked content", "product_id", productID.String(), "error", err)
		if isDependencyTimeout(err) {
			writeDependencyTimeout(w)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "content_generation_failed"})
		return
	}
	factChecker, err := s.factCheckerForRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
		return
	}
	factCheck, err := factChecker.Check(r.Context(), result.GeneratedContent)
	if err != nil {
		s.log.Error("fact check generated content", "product_id", productID.String(), "error", err)
		if isDependencyTimeout(err) {
			writeDependencyTimeout(w)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "fact_check_failed"})
		return
	}
	factCheck.ID = uuid.NewString()
	factCheck.ProductID = productID.String()
	s.storeFactCheckResult(r, factCheck)
	evaluation := normalizeContentEvaluation(result.Evaluation)
	pass := evaluation.Pass && factCheck.Pass

	writeJSON(w, http.StatusOK, contentFactCheckResponse{
		FactCheckID:     factCheck.ID,
		ProductID:       productID.String(),
		Description:     result.Description,
		SEOTitle:        result.SEOTitle,
		MetaDescription: result.MetaDescription,
		Score:           evaluation.Score,
		Pass:            pass,
		TokensUsed:      result.TokensUsed,
		Evaluation:      evaluation,
		FactCheck:       factCheck,
	})
}

func (s *server) getFactCheckResult(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/content/fact-checks/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_fact_check_id"})
		return
	}
	result, ok := s.loadFactCheckResult(r, id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) storeFactCheckResult(r *http.Request, result contentagent.FactCheckResult) {
	entry := storedFactCheckResult{result: result}
	if tenantID, scoped, err := s.tenantIDForScopedRequest(r); err == nil && scoped {
		entry.tenantID = string(tenantID)
	}
	s.factChecksMu.Lock()
	defer s.factChecksMu.Unlock()
	if s.factChecks == nil {
		s.factChecks = map[string]storedFactCheckResult{}
	}
	s.factChecks[result.ID] = entry
}

func (s *server) loadFactCheckResult(r *http.Request, id string) (contentagent.FactCheckResult, bool) {
	s.factChecksMu.RLock()
	defer s.factChecksMu.RUnlock()
	stored, ok := s.factChecks[id]
	if !ok {
		return contentagent.FactCheckResult{}, false
	}
	if stored.tenantID == "" {
		return stored.result, true
	}
	tenantID, scoped, err := s.tenantIDForScopedRequest(r)
	if err != nil || !scoped || string(tenantID) != stored.tenantID {
		return contentagent.FactCheckResult{}, false
	}
	return stored.result, true
}

func (s *server) factCheckerForRequest(r *http.Request) (*contentagent.FactChecker, error) {
	tenantID, scoped, err := s.tenantIDForScopedRequest(r)
	if err != nil {
		return nil, err
	}
	if !scoped || s.rag == nil {
		return s.factChecker, nil
	}
	return contentagent.NewFactChecker(tenantScopedEvidenceSearcher{
		service:  s.rag,
		tenantID: string(tenantID),
	}, contentagent.FactCheckOptions{}), nil
}

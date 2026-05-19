package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/compliance"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	"github.com/nfsarch33/helixon-ec/internal/seo"
	tenantpkg "github.com/nfsarch33/helixon-ec/internal/tenant"
)

type complianceCheckRequest struct {
	Keywords        []string `json:"keywords,omitempty"`
	SEOTitle        string   `json:"seo_title,omitempty"`
	MetaDescription string   `json:"meta_description,omitempty"`
	SEOScoreMin     int      `json:"seo_score_min,omitempty"`
	LegalDisclaimer string   `json:"legal_disclaimer,omitempty"`
}

type complianceCheckResponse struct {
	ProductID string                  `json:"product_id"`
	Pass      bool                    `json:"pass"`
	Score     int                     `json:"score"`
	Reasons   []string                `json:"reasons"`
	RuleIDs   []string                `json:"rule_ids"`
	Severity  compliance.Severity     `json:"severity"`
	Results   []compliance.RuleResult `json:"results"`
}

type complianceRulesResponse struct {
	Rules []compliance.RuleDescriptor `json:"rules"`
}

type seoSuggestionRequest struct {
	Keywords []string `json:"keywords,omitempty"`
}

type seoSuggestionResponse struct {
	ProductID       string             `json:"product_id"`
	Title           string             `json:"title"`
	MetaDescription string             `json:"meta_description"`
	Slug            string             `json:"slug"`
	Score           int                `json:"score"`
	KeywordDensity  map[string]float64 `json:"keyword_density"`
	Pass            bool               `json:"pass"`
	Reasons         []string           `json:"reasons"`
}

func (s *server) complianceRulesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	engine := compliance.NewEngine(compliance.DefaultRules())
	writeJSON(w, http.StatusOK, complianceRulesResponse{Rules: engine.Rules()})
}

func (s *server) complianceCheck(w http.ResponseWriter, r *http.Request, path string) {
	var req complianceCheckRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	product, ok := s.productByActionPath(w, r, path, "/compliance-check")
	if !ok {
		return
	}
	s.ensureTenantServices()
	tenantID, err := s.tenantIDForRequest(r, false)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
		return
	}
	ctx := tenantpkg.WithID(r.Context(), tenantID)
	settings, _ := s.tenantService.GetSettings(ctx, tenantID)
	if req.SEOScoreMin == 0 && settings.Compliance.SEOScoreMin > 0 {
		req.SEOScoreMin = settings.Compliance.SEOScoreMin
	}
	rules := compliance.ApplyOverrides(compliance.DefaultRules(), settings.Compliance.DisabledRuleIDs, settings.Compliance.SeverityOverride)
	if s.customRuleStore != nil {
		customRules, err := s.customRuleStore.ListCustomRules(ctx, string(tenantID))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		for _, rule := range customRules {
			rules = append(rules, rule)
		}
	}
	engine := compliance.NewEngine(rules)
	result := engine.Evaluate(ctx, compliance.ProductContent{
		Product:         product,
		Keywords:        req.Keywords,
		SEOTitle:        req.SEOTitle,
		Meta:            req.MetaDescription,
		SEOScoreMin:     req.SEOScoreMin,
		LegalDisclaimer: req.LegalDisclaimer,
	})
	if s.complianceHistory != nil {
		_ = s.complianceHistory.RecordEvaluation(ctx, compliance.EvaluationRecord{
			TenantID:  string(tenantID),
			ProductID: product.ID().String(),
			CheckedAt: time.Now().UTC(),
			Result:    result,
		})
	}
	writeJSON(w, http.StatusOK, complianceCheckResponse{
		ProductID: product.ID().String(),
		Pass:      result.Pass,
		Score:     result.Score,
		Reasons:   result.Reasons,
		RuleIDs:   result.RuleIDs,
		Severity:  result.Severity,
		Results:   result.Results,
	})
}

func (s *server) seoSuggestions(w http.ResponseWriter, r *http.Request, path string) {
	var req seoSuggestionRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	product, ok := s.productByActionPath(w, r, path, "/seo-suggestions")
	if !ok {
		return
	}
	suggestion := seo.NewOptimizer().Suggest(seo.Input{
		Title:       product.Title(),
		Description: product.Description(),
		Keywords:    req.Keywords,
	})
	writeJSON(w, http.StatusOK, seoSuggestionResponse{
		ProductID:       product.ID().String(),
		Title:           suggestion.Title,
		MetaDescription: suggestion.MetaDescription,
		Slug:            suggestion.Slug,
		Score:           suggestion.Score,
		KeywordDensity:  suggestion.KeywordDensity,
		Pass:            suggestion.Pass,
		Reasons:         suggestion.Reasons,
	})
}

func (s *server) productByActionPath(w http.ResponseWriter, r *http.Request, path, suffix string) (catalog.Product, bool) {
	idPart := strings.Trim(strings.TrimSuffix(path, suffix), "/")
	id, err := uuid.Parse(idPart)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return catalog.Product{}, false
	}
	product, err := s.productForRequest(r, id.String())
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return catalog.Product{}, false
		}
		s.log.Error("get product for compliance", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return catalog.Product{}, false
	}
	return product, true
}

func decodeOptionalJSON(r *http.Request, out any) error {
	if r.Body == nil {
		return nil
	}
	err := json.NewDecoder(r.Body).Decode(out)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

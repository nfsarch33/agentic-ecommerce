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
)

type fakeContentAgent struct {
	result   content.GenerateResult
	err      error
	captured []content.GenerateRequest
}

func (f *fakeContentAgent) Generate(_ context.Context, req content.GenerateRequest) (content.GenerateResult, error) {
	f.captured = append(f.captured, req)
	if f.err != nil {
		return content.GenerateResult{}, f.err
	}
	return f.result, nil
}

func TestGenerateDescriptionEndpointRunsContentAgent(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "RB-SET-5", "Resistance Band Set", 4995)
	srv.contentAgent = &fakeContentAgent{result: content.GenerateResult{
		GeneratedContent: content.GeneratedContent{
			Description:     "Resistance Band Set supports progressive home workouts.",
			SEOTitle:        "Resistance Band Set for Home Workouts",
			MetaDescription: "Shop a resistance band set for progressive home workouts.",
		},
		Evaluation: content.Evaluation{Score: 88, Pass: true},
		TokensUsed: 77,
	}}

	body := `{"style":"casual","max_words":80,"keywords":["resistance band set","home workouts"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+product.ID().String()+"/generate-description", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp contentSuggestionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ProductID != product.ID().String() || resp.Description == "" || resp.Score != 88 {
		t.Fatalf("response = %+v", resp)
	}
	fake := srv.contentAgent.(*fakeContentAgent)
	if len(fake.captured) != 1 {
		t.Fatalf("captured requests = %d, want 1", len(fake.captured))
	}
	if fake.captured[0].Style != content.StyleCasual || fake.captured[0].Product.SKU != "RB-SET-5" {
		t.Fatalf("captured request = %+v", fake.captured[0])
	}
}

func TestAISuggestionsEndpointUsesDefaultConstraints(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "FOAM-ROLLER", "Foam Roller", 3500)
	srv.contentAgent = &fakeContentAgent{result: content.GenerateResult{
		GeneratedContent: content.GeneratedContent{
			Description:     "Dense foam roller for recovery and mobility.",
			SEOTitle:        "Foam Roller for Recovery",
			MetaDescription: "Improve mobility with a dense foam roller for recovery work.",
		},
		Evaluation: content.Evaluation{Score: 84, Pass: true},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/"+product.ID().String()+"/ai-suggestions", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	fake := srv.contentAgent.(*fakeContentAgent)
	if got := fake.captured[0].Style; got != content.StyleProfessional {
		t.Fatalf("default style = %q, want professional", got)
	}
	if fake.captured[0].MaxWords != 120 {
		t.Fatalf("default max words = %d, want 120", fake.captured[0].MaxWords)
	}
}

func TestContentEndpointsReturnServiceUnavailableWhenAgentMissing(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "RB-SET-5", "Resistance Band Set", 4995)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+product.ID().String()+"/generate-description", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestContentEndpointsMapAgentFailureToBadGateway(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProduct(t, repo, "RB-SET-5", "Resistance Band Set", 4995)
	srv.contentAgent = &fakeContentAgent{err: errors.New("bridge unavailable")}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/"+product.ID().String()+"/ai-suggestions", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
}

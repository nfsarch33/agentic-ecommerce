package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

func TestComplianceCheckEndpointReturnsRuleBreakdown(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProductWithContent(t, repo, catalog.ProductInput{
		SKU:         "RB-SET-5",
		Title:       "Premium Resistance Band Set",
		Description: "Premium resistance band set for home workouts, warm ups, rehab, and progressive strength training.",
		Images:      []catalog.Image{{URL: "https://cdn.example.com/rb.jpg", Alt: "Premium resistance band set with handles"}},
	})
	body := `{"keywords":["resistance band set","home workouts"],"seo_title":"Premium Resistance Band Set for Home Workouts","meta_description":"Premium resistance band set for home workouts, warm ups, rehab, and progressive strength training.","seo_score_min":70}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+product.ID().String()+"/compliance-check", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp complianceCheckResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Pass || resp.Score < 90 {
		t.Fatalf("response = %+v, want passing high score", resp)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected per-rule results")
	}
}

func TestComplianceRulesEndpointListsBuiltInRules(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/rules", nil)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp complianceRulesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Rules) != 6 {
		t.Fatalf("rules = %d, want 6: %#v", len(resp.Rules), resp.Rules)
	}
	if resp.Rules[0].ID != "required_title" {
		t.Fatalf("first rule = %#v", resp.Rules[0])
	}
}

func TestSEOSuggestionsEndpointReturnsDeterministicSuggestions(t *testing.T) {
	t.Parallel()

	srv, repo := testServer(t)
	product := addProductWithContent(t, repo, catalog.ProductInput{
		SKU:         "RB-SET-5",
		Title:       "Premium Resistance Band Set for Home Workouts",
		Description: "Premium resistance band set for strength training at home. This resistance band set supports warm ups, rehab, and full body workouts.",
	})
	body := `{"keywords":["resistance band set","home workouts"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+product.ID().String()+"/seo-suggestions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp seoSuggestionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ProductID != product.ID().String() {
		t.Fatalf("product_id = %q, want %q", resp.ProductID, product.ID().String())
	}
	if resp.Slug != "premium-resistance-band-set-for-home-workouts" || resp.Score != 100 {
		t.Fatalf("response = %+v", resp)
	}
}

func addProductWithContent(t *testing.T, repo productCreator, input catalog.ProductInput) catalog.Product {
	t.Helper()
	price, _ := catalog.NewMoney(4995, "AUD")
	input.Price = price
	input.Stock = 10
	input.Status = catalog.StatusActive
	product, err := catalog.NewProduct(input)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if err := repo.Create(t.Context(), product); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	return product
}

type productCreator interface {
	Create(ctx context.Context, product catalog.Product) error
}

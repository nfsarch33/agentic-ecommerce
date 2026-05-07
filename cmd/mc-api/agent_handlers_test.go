package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	orchestrator "github.com/nfsarch33/agentic-ecommerce/internal/agent"
)

func TestReadyz(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	agentWorker, _ := got["agent_worker"].(map[string]any)
	if got["status"] != "ready" || got["agents"] != float64(3) || agentWorker["ready"] != true {
		t.Fatalf("readyz body = %#v", got)
	}
}

func TestAgentsListEndpoint(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp agentsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Agents) != 3 {
		t.Fatalf("agents len = %d, want 3: %#v", len(resp.Agents), resp.Agents)
	}
	if resp.Agents[0].ID != "compliance" || resp.Agents[1].ID != "pricing" || resp.Agents[2].ID != "sourcing" {
		t.Fatalf("agents sorted by ID = %#v", resp.Agents)
	}
}

func TestAgentRunEndpointSchedulesAndExposesHistory(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	body := `{
		"priority": 4,
		"payload": {
			"sku": "RB-SET",
			"cost_cents": 1800,
			"shipping_cents": 250,
			"current_price_cents": 4995,
			"competitor_prices_cents": [4595, 4895, 5195],
			"target_margin_pct": 0.45,
			"minimum_margin_pct": 0.32
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/pricing/run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	var submitted agentRunResponse
	if err := json.NewDecoder(rec.Body).Decode(&submitted); err != nil {
		t.Fatalf("decode submitted: %v", err)
	}
	if submitted.ID == "" || submitted.AgentID != "pricing" {
		t.Fatalf("submitted response = %#v", submitted)
	}

	completed, err := srv.agentScheduler.Wait(req.Context(), submitted.ID)
	if err != nil {
		t.Fatalf("wait run: %v", err)
	}
	if completed.State != orchestrator.RunSucceeded {
		t.Fatalf("completed state = %s, want succeeded", completed.State)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent-runs/"+submitted.ID, nil)
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get run status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got agentRunResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if got.State != string(orchestrator.RunSucceeded) || got.Result["recommended_price_cents"] == nil {
		t.Fatalf("run response = %#v", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/agents/pricing/history", nil)
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("history status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var history agentHistoryResponse
	if err := json.NewDecoder(rec.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history.Runs) != 1 || history.Runs[0].ID != submitted.ID {
		t.Fatalf("history = %#v", history)
	}
}

func TestAgentRunEndpointRejectsMissingAgent(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/missing/run", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

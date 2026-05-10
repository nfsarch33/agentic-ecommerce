package compliance

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestComplianceHandler_DeleteRequest(t *testing.T) {
	t.Parallel()
	repo := &stubRepo{tables: []string{"customers", "orders"}}
	svc := NewService(repo, nil, func() time.Time { return fixedNow })
	handler := NewComplianceHandler(svc, nil)

	body := `{"subject_id":"subj-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/delete-request", bytes.NewBufferString(body))
	req.Header.Set("X-Tenant-Id", "t1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "deleted" {
		t.Fatalf("status = %s, want deleted", resp["status"])
	}
}

func TestComplianceHandler_ExportRequest(t *testing.T) {
	t.Parallel()
	repo := &stubRepo{
		customerData: map[string]any{"name": "Jane"},
		orders:       []map[string]any{{"order_id": "o1"}},
	}
	svc := NewService(repo, nil, func() time.Time { return fixedNow })
	handler := NewComplianceHandler(svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/export/subj-1", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var bundle ExportBundle
	_ = json.NewDecoder(w.Body).Decode(&bundle)
	if bundle.CustomerData["name"] != "Jane" {
		t.Fatal("customer name missing from export")
	}
}

func TestComplianceHandler_MissingTenantHeader(t *testing.T) {
	t.Parallel()
	svc := NewService(&stubRepo{}, nil, func() time.Time { return fixedNow })
	handler := NewComplianceHandler(svc, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/delete-request", bytes.NewBufferString(`{"subject_id":"s1"}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestComplianceHandler_UnsupportedMethod(t *testing.T) {
	t.Parallel()
	svc := NewService(&stubRepo{}, nil, func() time.Time { return fixedNow })
	handler := NewComplianceHandler(svc, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/compliance/export/subj-1", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

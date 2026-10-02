package media

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// stubService is the media service contract in miniature: idempotent per
// job_id (same id -> same checksum, cached=true), a per-job render count
// so a duplicate render is OBSERVABLE, and a checksum that matches the
// bytes it returns.
type stubService struct {
	mu      sync.Mutex
	renders map[string]int // job_id -> render count
}

func newStub() *stubService { return &stubService{renders: map[string]int{}} }

func (s *stubService) handler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JobID    string   `json:"job_id"`
		ImageB64 string   `json:"image_b64"`
		Ops      []string `json:"ops"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.JobID == "" {
		http.Error(w, "job_id required", http.StatusBadRequest)
		return
	}
	// The cached path returns THE SAME bytes and checksum as the first
	// render (what the real service's cache does).
	out := base64.StdEncoding.EncodeToString([]byte("render-" + req.JobID))
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte("render-"+req.JobID)))
	if _, ok := s.renders[req.JobID]; ok {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job_id": req.JobID, "out_b64": out, "sha256": sum, "secs": 0.0, "cached": true,
		})
		return
	}
	s.renders[req.JobID] = 1
	_ = json.NewEncoder(w).Encode(map[string]any{
		"job_id": req.JobID, "out_b64": out, "sha256": sum, "secs": 0.5, "cached": false,
	})
}

// MUTANT: drop the job id from the request and every retry re-renders —
// the duplicate-render acceptance; this goes red on the second call.
func TestRetriesWithTheSameJobIDDoNotDuplicateRender(t *testing.T) {
	svc := newStub()
	srv := httptest.NewServer(http.HandlerFunc(svc.handler))
	defer srv.Close()
	c := NewClient(srv.URL)
	img := []byte("fixture-image-bytes")

	first, err := c.Process(context.Background(), JobID("SKU-1", OpRemoveBG), img, OpRemoveBG)
	if err != nil {
		t.Fatalf("first Process: %v", err)
	}
	if first.Cached {
		t.Fatal("the first call must be a real render")
	}
	second, err := c.Process(context.Background(), JobID("SKU-1", OpRemoveBG), img, OpRemoveBG)
	if err != nil {
		t.Fatalf("retry Process: %v", err)
	}
	if !second.Cached {
		t.Fatal("the retry must be answered by the idempotency cache")
	}
	if second.SHA256 != first.SHA256 {
		t.Fatalf("cached checksum drifted: %s vs %s", second.SHA256, first.SHA256)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if n := svc.renders[JobID("SKU-1", OpRemoveBG)]; n != 1 {
		t.Fatalf("the service rendered %d times for one job id, want exactly 1", n)
	}
}

// MUTANT: drop the checksum verification and a lying service passes.
func TestChecksumIsVerifiedAgainstTheReturnedBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		out := base64.StdEncoding.EncodeToString([]byte("real-bytes"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"out_b64": out, "sha256": "not-the-real-checksum", "secs": 0.1, "cached": false,
		})
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	if _, err := c.Process(context.Background(), "j-1", []byte("x"), OpRemoveBG); err == nil {
		t.Fatal("a lying checksum must fail the client")
	}
}

// JobID is deterministic for the same inputs and differs across them.
func TestJobIDIsDeterministic(t *testing.T) {
	if JobID("SKU-1", OpRemoveBG) != JobID("SKU-1", OpRemoveBG) {
		t.Fatal("same inputs must give the same job id")
	}
	if JobID("SKU-1", OpRemoveBG) == JobID("SKU-2", OpRemoveBG) {
		t.Fatal("different SKUs must give different job ids")
	}
	if JobID("SKU-1", OpRemoveBG) == JobID("SKU-1", OpRemoveBG, OpUpscale) {
		t.Fatal("a different op set must give a different job id")
	}
}

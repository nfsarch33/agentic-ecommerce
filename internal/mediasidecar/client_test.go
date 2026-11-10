package mediasidecar

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeSidecar serves /v1/process per spec 2.3: it computes the edited
// bytes deterministically from the job id, reports their real sha256, and
// answers cached=true with the identical checksum when it has seen the
// job id before. mode "lying" reports the sha256 of other bytes.
type fakeSidecar struct {
	mu     sync.Mutex
	hits   int
	jobIDs []string
	ops    [][]string
	seen   map[string]bool
	mode   string
}

func newFakeSidecar(mode string) *fakeSidecar {
	return &fakeSidecar{seen: map[string]bool{}, mode: mode}
}

func (f *fakeSidecar) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/process" {
			t.Fatalf("sidecar saw %s %s, want POST /v1/process", r.Method, r.URL.Path)
		}
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("sidecar decode: %v", err)
		}
		f.mu.Lock()
		f.hits++
		f.jobIDs = append(f.jobIDs, req.JobID)
		f.ops = append(f.ops, req.Ops)
		cached := f.seen[req.JobID]
		f.seen[req.JobID] = true
		f.mu.Unlock()
		out := []byte("edited:" + req.JobID)
		sum := sha256.Sum256(out)
		if f.mode == "lying" {
			sum = sha256.Sum256([]byte("other-bytes-entirely"))
		}
		_ = json.NewEncoder(w).Encode(Response{
			OutB64: base64.StdEncoding.EncodeToString(out),
			SHA256: hex.EncodeToString(sum[:]),
			Secs:   0.01,
			Cached: cached,
		})
	})
}

// S6: the client verifies the reported checksum against the returned
// bytes - a sidecar that lies about its sha256 is refused.
func TestProcess_VerifiesChecksumMismatch(t *testing.T) {
	f := newFakeSidecar("lying")
	server := httptest.NewServer(f.handler(t))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, server.Client())
	_, err := client.Process(context.Background(), "job-1", base64.StdEncoding.EncodeToString([]byte("src")), []string{"bg", "upscale"})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
}

// S6: the same job_id sent twice reports cached true the second time and
// the identical, still-verified checksum both times.
func TestProcess_CacheReplaySameChecksum(t *testing.T) {
	f := newFakeSidecar("honest")
	server := httptest.NewServer(f.handler(t))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, server.Client())
	first, err := client.Process(context.Background(), "job-7", base64.StdEncoding.EncodeToString([]byte("src")), []string{"bg", "upscale"})
	if err != nil {
		t.Fatalf("first process: %v", err)
	}
	second, err := client.Process(context.Background(), "job-7", base64.StdEncoding.EncodeToString([]byte("src")), []string{"bg", "upscale"})
	if err != nil {
		t.Fatalf("second process: %v", err)
	}
	if !second.Cached {
		t.Fatal("the second call for the same job_id must report cached true")
	}
	if first.SHA256 != second.SHA256 {
		t.Fatalf("checksum moved across the replay: %s then %s", first.SHA256, second.SHA256)
	}
	if f.hits != 2 {
		t.Fatalf("sidecar hits = %d, want 2", f.hits)
	}
}

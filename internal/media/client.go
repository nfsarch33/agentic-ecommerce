// Package media drives the image media service (background removal and
// upscale) for product content. Jobs carry IDEMPOTENT ids — the service
// caches per job_id, and a retry with the same id returns the cached
// result instead of a duplicate render (the acceptance's mutant).
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client talks to the media service.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient builds a client (empty baseURL is answered by the caller's
// wiring decision, not defaulted here).
func NewClient(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 120 * time.Second}}
}

// JobID mints the idempotent job id for one (sku, op-set): the same
// inputs always produce the same id, so a retry re-enters the service's
// cache instead of rendering twice. The ops are HASHED, not embedded:
// the id stays short and any op order change changes the hash.
func JobID(sku string, ops ...Op) string {
	h := sha256.New()
	for _, op := range ops {
		_, _ = h.Write([]byte(op))
		_, _ = h.Write([]byte{0})
	}
	return fmt.Sprintf("media-%s-%x", sku, h.Sum(nil)[:8])
}

// Op is one media operation.
type Op string

const (
	OpRemoveBG Op = "bg"
	OpUpscale  Op = "upscale"
)

// ProcessResult is one processed image (the package already has a
// validation Result — this is the processing outcome).
type ProcessResult struct {
	SHA256 string
	Secs   float64
	Cached bool
	Ops    []Op
}

type processRequest struct {
	JobID    string   `json:"job_id"`
	ImageB64 string   `json:"image_b64"`
	Ops      []string `json:"ops"`
}

type processResponse struct {
	OutB64 string   `json:"out_b64"`
	SHA256 string   `json:"sha256"`
	Secs   float64  `json:"secs"`
	Cached bool     `json:"cached"`
	Ops    []string `json:"ops"`
}

// Process sends one image through the ops and returns the outcome. The
// image bytes are returned decoded-free: the caller gets the checksum,
// the wall time the service spent, and whether the idempotency cache
// answered (a retry MUST report Cached=true with the same checksum).
func (c *Client) Process(ctx context.Context, jobID string, image []byte, ops ...Op) (ProcessResult, error) {
	opsList := make([]string, len(ops))
	for i, op := range ops {
		opsList[i] = string(op)
	}
	body, err := json.Marshal(processRequest{
		JobID:    jobID,
		ImageB64: base64.StdEncoding.EncodeToString(image),
		Ops:      opsList,
	})
	if err != nil {
		return ProcessResult{}, fmt.Errorf("media: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/process", bytes.NewReader(body))
	if err != nil {
		return ProcessResult{}, fmt.Errorf("media: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("media: call service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ProcessResult{}, fmt.Errorf("media: service status %d", resp.StatusCode)
	}
	var out processResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ProcessResult{}, fmt.Errorf("media: decode response: %w", err)
	}
	got := ProcessResult{SHA256: out.SHA256, Secs: out.Secs, Cached: out.Cached}
	for _, op := range out.Ops {
		got.Ops = append(got.Ops, Op(op))
	}
	// The service's checksum is the contract: verify it against the bytes
	// it returned so a lying cache cannot pass silently.
	dec, err := base64.StdEncoding.DecodeString(out.OutB64)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("media: decode image: %w", err)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(dec))
	if sum != out.SHA256 {
		return ProcessResult{}, fmt.Errorf("media: checksum mismatch (claimed %s, got %s)", out.SHA256, sum)
	}
	return got, nil
}

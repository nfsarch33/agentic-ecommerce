// Package mediasidecar talks to the EC media sidecar's /v1/process
// endpoint (spec 2.3): one call carries a job id, the source image as
// base64 and the ops; the answer carries the edited image, its sha256,
// the seconds it took and whether the job id hit the sidecar's cache.
// The client verifies the reported checksum against the bytes it got, so
// a lying cache cannot pass silently.
package mediasidecar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrChecksumMismatch is returned when the sidecar's reported sha256 does
// not match the bytes it returned.
var ErrChecksumMismatch = errors.New("mediasidecar: reported sha256 does not match the returned bytes")

// Request is the /v1/process body.
type Request struct {
	JobID    string   `json:"job_id"`
	ImageB64 string   `json:"image_b64"`
	Ops      []string `json:"ops"`
}

// Response is the sidecar's answer.
type Response struct {
	OutB64 string  `json:"out_b64"`
	SHA256 string  `json:"sha256"`
	Secs   float64 `json:"secs"`
	Cached bool    `json:"cached"`
}

// Client calls one sidecar base URL.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient builds a client for one sidecar base URL.
func NewClient(baseURL string, httpClient *http.Client) Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return Client{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

// Process posts one job and verifies the reported sha256 against the
// returned bytes before answering.
func (c Client) Process(ctx context.Context, jobID, imageB64 string, ops []string) (Response, error) {
	body, err := json.Marshal(Request{JobID: jobID, ImageB64: imageB64, Ops: ops})
	if err != nil {
		return Response{}, fmt.Errorf("mediasidecar: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/process", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("mediasidecar: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("mediasidecar: send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Response{}, fmt.Errorf("mediasidecar: status %d", resp.StatusCode)
	}
	var out Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Response{}, fmt.Errorf("mediasidecar: decode response: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(out.OutB64)
	if err != nil {
		return Response{}, fmt.Errorf("mediasidecar: decode out_b64: %w", err)
	}
	sum := sha256.Sum256(raw)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), out.SHA256) {
		return Response{}, ErrChecksumMismatch
	}
	return out, nil
}

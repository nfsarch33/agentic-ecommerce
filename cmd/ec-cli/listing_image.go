package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/mediasidecar"
)

// maxSourceImageBytes caps the source fetch so an oversized draft image
// cannot blow the step's memory or the S9 budget.
const maxSourceImageBytes = 4 << 20

// runImageStep runs the spec 2.3 image edit when EC_MEDIA_URL is set:
// fetch the source image, POST it to the media sidecar's /v1/process
// with ops bg+upscale under a job id derived from the SKU and the source
// checksum, verify the returned sha256 against the returned bytes, and
// hand back a data-URL for the edited image. Any failure - source fetch,
// sidecar outage, checksum mismatch - publishes the source image and
// records image_edited false; a media outage never blocks a listing (S9).
func runImageStep(ctx context.Context, d listingDraft, deps appDeps) (editedImageURL string, note string) {
	base := deps.getenv("EC_MEDIA_URL")
	if base == "" {
		return "", ""
	}
	stepCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	src, err := fetchSourceImage(stepCtx, d.ImageSrc)
	if err != nil {
		return "", "IMAGE edited=false reason=source-fetch-failed"
	}
	sum := sha256.Sum256(src)
	srcHex := hex.EncodeToString(sum[:])
	resp, err := mediasidecar.NewClient(base, &http.Client{}).Process(stepCtx, imageJobID(d.SKU, srcHex), base64.StdEncoding.EncodeToString(src), []string{"bg", "upscale"})
	switch {
	case err == nil:
		return "data:image/png;base64," + resp.OutB64, fmt.Sprintf("IMAGE edited=true sha256=%s cached=%t", resp.SHA256, resp.Cached)
	case errors.Is(err, mediasidecar.ErrChecksumMismatch):
		return "", "IMAGE edited=false sha256=" + srcHex + " reason=checksum-mismatch"
	default:
		return "", "IMAGE edited=false sha256=" + srcHex + " reason=media-outage"
	}
}

// imageJobID derives the sidecar job id for one listing image: the SKU
// plus the source checksum, so a retry of the same draft replays the same
// job id and the sidecar answers from its cache with the same checksum.
func imageJobID(sku, sourceSHA256Hex string) string {
	if len(sourceSHA256Hex) > 12 {
		sourceSHA256Hex = sourceSHA256Hex[:12]
	}
	return "e2-" + strings.ToLower(sku) + "-" + sourceSHA256Hex
}

// fetchSourceImage downloads the draft's source image bytes, capped.
func fetchSourceImage(ctx context.Context, imageSrc string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageSrc, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source image status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxSourceImageBytes {
		return nil, fmt.Errorf("source image over %d bytes", maxSourceImageBytes)
	}
	return raw, nil
}

package intelligence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/media"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

const (
	defaultMaxSourceBytes = 10 * 1024 * 1024
	defaultMinAspectRatio = 0.75
	defaultMaxAspectRatio = 1.50
)

var (
	ErrHTTPClientRequired = errors.New("media intelligence http client is required")
	ErrInvalidSourceURL   = errors.New("invalid media source url")
	ErrSourceFailed       = errors.New("media source request failed")
	ErrMediaNotFound      = errors.New("media asset not found")
	ErrStoreRequired      = errors.New("media object store is required")
)

type MediaObject = port.MediaObject
type StoredMediaObject = port.StoredMediaObject

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Store interface {
	Put(context.Context, port.MediaObject) (port.StoredMediaObject, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type ServiceConfig struct {
	HTTPClient     HTTPClient
	Store          Store
	MaxSourceBytes int64
	Now            func() time.Time
}

type Service struct {
	client         HTTPClient
	store          Store
	maxSourceBytes int64
	now            func() time.Time

	mu     sync.RWMutex
	assets map[string]Asset
}

type SourceRequest struct {
	URL       string `json:"url"`
	ProductID string `json:"product_id,omitempty"`
	AltText   string `json:"alt_text,omitempty"`
}

type ProcessRequest struct {
	MediaID          string        `json:"media_id"`
	Resize           ResizeOptions `json:"resize,omitempty"`
	Format           string        `json:"format,omitempty"`
	RemoveBackground bool          `json:"remove_background,omitempty"`
}

type ResizeOptions struct {
	MaxWidth  int `json:"max_width,omitempty"`
	MaxHeight int `json:"max_height,omitempty"`
}

type Asset struct {
	ID         string         `json:"id"`
	ProductID  string         `json:"product_id,omitempty"`
	SourceURL  string         `json:"source_url,omitempty"`
	AltText    string         `json:"alt_text,omitempty"`
	Metadata   Metadata       `json:"metadata"`
	Processing ProcessingInfo `json:"processing,omitempty"`
	Quality    QualityReport  `json:"quality,omitempty"`
	Storage    StorageInfo    `json:"storage,omitempty"`
	CreatedAt  time.Time      `json:"created_at,omitempty"`
	payload    []byte
}

type Metadata struct {
	MimeType       string `json:"mime_type"`
	ContentLength  int64  `json:"content_length"`
	ChecksumSHA256 string `json:"checksum_sha256"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
}

type ProcessingInfo struct {
	Operations []ProcessingOperation `json:"operations,omitempty"`
}

type ProcessingOperation struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type QualityReport struct {
	Pass   bool           `json:"pass"`
	Score  int            `json:"score"`
	Issues []QualityIssue `json:"issues,omitempty"`
}

type QualityIssue struct {
	ID       string `json:"id"`
	Message  string `json:"message"`
	Severity string `json:"severity,omitempty"`
	Blocking bool   `json:"blocking"`
}

type StorageInfo struct {
	Key         string `json:"key,omitempty"`
	URL         string `json:"url,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
}

func NewService(cfg ServiceConfig) *Service {
	maxBytes := cfg.MaxSourceBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxSourceBytes
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		client:         cfg.HTTPClient,
		store:          cfg.Store,
		maxSourceBytes: maxBytes,
		now:            now,
		assets:         map[string]Asset{},
	}
}

func (s *Service) Source(ctx context.Context, req SourceRequest) (Asset, error) {
	sourceURL, err := validateSourceURL(req.URL)
	if err != nil {
		return Asset{}, err
	}
	if s.client == nil {
		return Asset{}, ErrHTTPClientRequired
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return Asset{}, fmt.Errorf("create source request: %w", err)
	}
	httpReq.Header.Set("User-Agent", "agentic-ecommerce-media-intelligence/1.4")
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return Asset{}, fmt.Errorf("%w: %v", ErrSourceFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Asset{}, fmt.Errorf("%w: status %d", ErrSourceFailed, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, s.maxSourceBytes+1))
	if err != nil {
		return Asset{}, fmt.Errorf("read source image: %w", err)
	}
	if int64(len(body)) > s.maxSourceBytes {
		return Asset{}, fmt.Errorf("%w: source image exceeds %d bytes", ErrSourceFailed, s.maxSourceBytes)
	}
	metadata := extractMetadata(body, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Length"))
	asset := Asset{
		ID:        mediaID(metadata.ChecksumSHA256),
		ProductID: strings.TrimSpace(req.ProductID),
		SourceURL: sourceURL,
		AltText:   strings.TrimSpace(req.AltText),
		Metadata:  metadata,
		CreatedAt: s.now(),
		payload:   body,
	}
	s.save(asset)
	return asset, nil
}

func (s *Service) Process(ctx context.Context, req ProcessRequest) (Asset, error) {
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	source, ok := s.Get(req.MediaID)
	if !ok {
		return Asset{}, ErrMediaNotFound
	}
	targetMime := normalizeOutputFormat(req.Format)
	if targetMime == "" {
		targetMime = source.Metadata.MimeType
	}
	width, height := resizedDimensions(source.Metadata.Width, source.Metadata.Height, req.Resize)
	operations := processingOperations(source.Metadata, targetMime, req)
	payload := deterministicProcessedPayload(source.payload, operations)
	metadata := Metadata{
		MimeType:       targetMime,
		ContentLength:  int64(len(payload)),
		ChecksumSHA256: checksum(payload),
		Width:          width,
		Height:         height,
	}
	processed := source
	processed.ID = mediaID(metadata.ChecksumSHA256)
	processed.Metadata = metadata
	processed.Processing = ProcessingInfo{Operations: operations}
	processed.CreatedAt = s.now()
	processed.payload = payload
	processed.Quality = QualityReport{}
	processed.Storage = StorageInfo{}
	s.save(processed)
	return processed, nil
}

func (s *Service) AssessQuality(asset Asset) QualityReport {
	constraints := media.DefaultConstraints()
	issues := make([]QualityIssue, 0)
	if asset.Metadata.Width > 0 && asset.Metadata.Height > 0 {
		if asset.Metadata.Width < constraints.MinWidth || asset.Metadata.Height < constraints.MinHeight {
			issues = append(issues, blockingIssue("resolution_too_small", "image dimensions are below the minimum display size"))
		}
		ratio := float64(asset.Metadata.Width) / float64(asset.Metadata.Height)
		if ratio < defaultMinAspectRatio || ratio > defaultMaxAspectRatio {
			issues = append(issues, blockingIssue("aspect_ratio_out_of_range", "image aspect ratio is outside the ecommerce display range"))
		}
	}
	mimeType := normalizeMimeType(asset.Metadata.MimeType)
	if _, ok := constraints.AllowedMimeTypes[mimeType]; !ok {
		issues = append(issues, blockingIssue("unsupported_format", "image format must be jpeg, png, webp, or gif"))
	}
	if asset.Metadata.ContentLength > constraints.MaxSizeBytes {
		issues = append(issues, blockingIssue("image_too_large", "image exceeds the maximum allowed size"))
	}
	for _, reason := range media.ValidateAltText(asset.AltText, "").Reasons {
		issues = append(issues, blockingIssue(reason.ID, reason.Message))
	}
	issues = append(issues, QualityIssue{
		ID:       "brand_safety_pending",
		Message:  "brand safety classifier is pending ML integration",
		Severity: "info",
		Blocking: false,
	})
	return qualityFromIssues(issues)
}

func (s *Service) Validate(ctx context.Context, mediaID string) (QualityReport, error) {
	if err := ctx.Err(); err != nil {
		return QualityReport{}, err
	}
	asset, ok := s.Get(mediaID)
	if !ok {
		return QualityReport{}, ErrMediaNotFound
	}
	qa := s.AssessQuality(asset)
	asset.Quality = qa
	s.save(asset)
	return qa, nil
}

func (s *Service) Store(ctx context.Context, mediaID string) (Asset, error) {
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	if s.store == nil {
		return Asset{}, ErrStoreRequired
	}
	asset, ok := s.Get(mediaID)
	if !ok {
		return Asset{}, ErrMediaNotFound
	}
	key := objectKey(asset)
	stored, err := s.store.Put(ctx, port.MediaObject{
		Key:         key,
		ContentType: asset.Metadata.MimeType,
		Body:        bytes.NewReader(asset.payload),
	})
	if err != nil {
		return Asset{}, err
	}
	asset.Storage = StorageInfo{
		Key:         stored.Key,
		URL:         stored.URL,
		ContentType: stored.ContentType,
		SizeBytes:   stored.SizeBytes,
	}
	s.save(asset)
	return asset, nil
}

func (s *Service) Get(mediaID string) (Asset, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	asset, ok := s.assets[mediaID]
	if !ok {
		return Asset{}, false
	}
	asset.payload = append([]byte(nil), asset.payload...)
	return asset, true
}

func (s *Service) save(asset Asset) {
	s.mu.Lock()
	defer s.mu.Unlock()
	asset.payload = append([]byte(nil), asset.payload...)
	s.assets[asset.ID] = asset
}

func validateSourceURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrInvalidSourceURL
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", ErrInvalidSourceURL
	}
	return parsed.String(), nil
}

func extractMetadata(body []byte, contentType, contentLength string) Metadata {
	mimeType := normalizeMimeType(contentType)
	if mimeType == "" {
		mimeType = normalizeMimeType(http.DetectContentType(body))
	}
	width, height := imageDimensions(body)
	length := int64(len(body))
	if headerLength, err := strconv.ParseInt(strings.TrimSpace(contentLength), 10, 64); err == nil && headerLength >= 0 && headerLength == int64(len(body)) {
		length = headerLength
	}
	return Metadata{
		MimeType:       mimeType,
		ContentLength:  length,
		ChecksumSHA256: checksum(body),
		Width:          width,
		Height:         height,
	}
}

func imageDimensions(body []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func checksum(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func mediaID(checksumSHA256 string) string {
	if len(checksumSHA256) >= 16 {
		return "media_" + checksumSHA256[:16]
	}
	return "media_" + checksumSHA256
}

func resizedDimensions(width, height int, opts ResizeOptions) (int, int) {
	if width <= 0 || height <= 0 {
		return width, height
	}
	scale := 1.0
	if opts.MaxWidth > 0 && width > opts.MaxWidth {
		scale = min(scale, float64(opts.MaxWidth)/float64(width))
	}
	if opts.MaxHeight > 0 && height > opts.MaxHeight {
		scale = min(scale, float64(opts.MaxHeight)/float64(height))
	}
	if scale >= 1 {
		return width, height
	}
	newWidth := int(float64(width) * scale)
	newHeight := int(float64(height) * scale)
	if newWidth < 1 {
		newWidth = 1
	}
	if newHeight < 1 {
		newHeight = 1
	}
	return newWidth, newHeight
}

func processingOperations(metadata Metadata, targetMime string, req ProcessRequest) []ProcessingOperation {
	ops := make([]ProcessingOperation, 0, 3)
	if req.Resize.MaxWidth > 0 || req.Resize.MaxHeight > 0 {
		ops = append(ops, ProcessingOperation{ID: "resize_stub", Message: "deterministic resize metadata stub applied"})
	}
	if targetMime != "" && targetMime != normalizeMimeType(metadata.MimeType) {
		ops = append(ops, ProcessingOperation{ID: "format_conversion_stub", Message: "deterministic format conversion stub applied"})
	}
	if req.RemoveBackground {
		ops = append(ops, ProcessingOperation{ID: "background_removal_todo", Message: "TODO: replace with ML background removal service"})
	}
	return ops
}

func deterministicProcessedPayload(payload []byte, operations []ProcessingOperation) []byte {
	if len(operations) == 0 {
		return append([]byte(nil), payload...)
	}
	var b bytes.Buffer
	b.Write(payload)
	b.WriteString("\nmedia-intelligence-stub:")
	for _, op := range operations {
		b.WriteString(op.ID)
		b.WriteByte(';')
	}
	return b.Bytes()
}

func normalizeOutputFormat(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "image/") {
		return normalizeMimeType(value)
	}
	switch strings.TrimPrefix(value, ".") {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	default:
		return normalizeMimeType(value)
	}
}

func normalizeMimeType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if idx := strings.Index(value, ";"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return value
}

func objectKey(asset Asset) string {
	productID := strings.TrimSpace(asset.ProductID)
	if productID == "" {
		productID = "unassigned"
	}
	ext := extensionForMime(asset.Metadata.MimeType)
	name := strings.TrimPrefix(asset.ID, "media_")
	return path.Join("products", productID, "media", name+ext)
}

func extensionForMime(mimeType string) string {
	switch normalizeMimeType(mimeType) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".bin"
	}
}

func blockingIssue(id, message string) QualityIssue {
	return QualityIssue{ID: id, Message: message, Severity: "error", Blocking: true}
}

func qualityFromIssues(issues []QualityIssue) QualityReport {
	blocking := 0
	for _, issue := range issues {
		if issue.Blocking {
			blocking++
		}
	}
	score := 100 - blocking*20
	if score < 0 {
		score = 0
	}
	return QualityReport{Pass: blocking == 0, Score: score, Issues: issues}
}

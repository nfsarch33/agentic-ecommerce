package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrImageEditUnconfigured = errors.New("media: image edit workflow unconfigured")
	ErrImageEditInvalid      = errors.New("media: image edit request invalid")
	ErrImageEditNotFound     = errors.New("media: image edit job not found")
	ErrImageEditState        = errors.New("media: image edit state invalid")
	ErrImageEditNoProvider   = errors.New("media: image edit provider unavailable")
	ErrImageEditFailed       = errors.New("media: image edit provider failed")
)

type ImageEditAction string

const (
	ImageEditActionBackgroundRemoval   ImageEditAction = "background_removal"
	ImageEditActionLifestyleGeneration ImageEditAction = "lifestyle_generation"
	ImageEditActionVariantGeneration   ImageEditAction = "variant_generation"
)

type ImageEditApprovalState string

const (
	ImageEditApprovalRequested ImageEditApprovalState = "requested"
	ImageEditApprovalPending   ImageEditApprovalState = "pending_approval"
	ImageEditApprovalApproved  ImageEditApprovalState = "approved"
	ImageEditApprovalRejected  ImageEditApprovalState = "rejected"
)

type ImageEditRequest struct {
	RequestID          string
	TenantID           string
	ProductID          string
	SourceURI          string
	Prompt             string
	Action             ImageEditAction
	SourceBytes        int64
	PreferredProviders []string
	RequiresApproval   bool
	AutoApprove        bool
}

type ImageEditProviderCapabilities struct {
	Remote bool
}

type ImageEditProviderResult struct {
	OutputURI          string
	OutputContentType  string
	OutputBytes        int
	ProviderRequestID  string
	ProviderDiagnostic string
}

type ImageEditProvider interface {
	Name() string
	Capabilities() ImageEditProviderCapabilities
	Edit(context.Context, ImageEditRequest) (ImageEditProviderResult, error)
}

type ImageEditJob struct {
	ID                 string
	TenantID           string
	ProductID          string
	Action             ImageEditAction
	SourceURI          string
	SourceBytes        int64
	Prompt             string
	ApprovalState      ImageEditApprovalState
	Provider           string
	AttemptedProviders []string
	OutputURI          string
	OutputContentType  string
	OutputBytes        int
	ProviderRequestID  string
	RejectReason       string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ImageEditMetric struct {
	Action      ImageEditAction
	Provider    string
	Status      string
	Duration    time.Duration
	SourceBytes int64
	OutputBytes int
}

type ImageEditMetricsHook func(ImageEditMetric)

type ImageEditWorkflowConfig struct {
	Providers        []ImageEditProvider
	MaxLocalBytes    int64
	Now              func() time.Time
	ImageEditMetrics ImageEditMetricsHook
}

type ImageEditWorkflow struct {
	providers        []ImageEditProvider
	maxLocalBytes    int64
	now              func() time.Time
	imageEditMetrics ImageEditMetricsHook

	mu   sync.Mutex
	jobs map[string]ImageEditJob
}

func NewImageEditWorkflow(cfg ImageEditWorkflowConfig) (*ImageEditWorkflow, error) {
	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("%w: at least one ImageEditProvider required", ErrImageEditUnconfigured)
	}
	seen := make(map[string]struct{}, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		if provider == nil {
			return nil, fmt.Errorf("%w: nil ImageEditProvider", ErrImageEditUnconfigured)
		}
		name := strings.TrimSpace(provider.Name())
		if name == "" {
			return nil, fmt.Errorf("%w: provider name required", ErrImageEditUnconfigured)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("%w: duplicate provider %q", ErrImageEditUnconfigured, name)
		}
		seen[name] = struct{}{}
	}
	maxLocalBytes := cfg.MaxLocalBytes
	if maxLocalBytes <= 0 {
		maxLocalBytes = MaxLocalDecodeBytes
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ImageEditWorkflow{
		providers:        append([]ImageEditProvider(nil), cfg.Providers...),
		maxLocalBytes:    maxLocalBytes,
		now:              now,
		imageEditMetrics: cfg.ImageEditMetrics,
		jobs:             map[string]ImageEditJob{},
	}, nil
}

func (w *ImageEditWorkflow) Request(ctx context.Context, req ImageEditRequest) (ImageEditJob, error) {
	if err := validateImageEditRequest(req); err != nil {
		return ImageEditJob{}, err
	}
	job := imageEditJobFromRequest(req, w.now())
	if req.RequiresApproval {
		job.ApprovalState = ImageEditApprovalPending
		return w.saveJob(job), nil
	}
	if req.AutoApprove {
		job.ApprovalState = ImageEditApprovalApproved
		return w.execute(ctx, job, req)
	}
	job.ApprovalState = ImageEditApprovalRequested
	return w.saveJob(job), nil
}

func (w *ImageEditWorkflow) Approve(ctx context.Context, id string) (ImageEditJob, error) {
	job, err := w.loadJob(id)
	if err != nil {
		return ImageEditJob{}, err
	}
	if job.ApprovalState == ImageEditApprovalRejected || job.ApprovalState == ImageEditApprovalApproved {
		return ImageEditJob{}, fmt.Errorf("%w: cannot approve job %q from state %q", ErrImageEditState, id, job.ApprovalState)
	}
	job.ApprovalState = ImageEditApprovalApproved
	return w.execute(ctx, job, requestFromImageEditJob(job))
}

func (w *ImageEditWorkflow) Reject(_ context.Context, id, reason string) (ImageEditJob, error) {
	job, err := w.loadJob(id)
	if err != nil {
		return ImageEditJob{}, err
	}
	if job.ApprovalState == ImageEditApprovalApproved {
		return ImageEditJob{}, fmt.Errorf("%w: cannot reject approved job %q", ErrImageEditState, id)
	}
	job.ApprovalState = ImageEditApprovalRejected
	job.RejectReason = strings.TrimSpace(reason)
	job.UpdatedAt = w.now().UTC()
	return w.saveJob(job), nil
}

func (w *ImageEditWorkflow) execute(ctx context.Context, job ImageEditJob, req ImageEditRequest) (ImageEditJob, error) {
	providers := w.orderedProviders(req)
	if len(providers) == 0 {
		return ImageEditJob{}, fmt.Errorf("%w: no provider matches request", ErrImageEditNoProvider)
	}
	var lastErr error
	start := w.now()
	for _, provider := range providers {
		if req.SourceBytes > w.maxLocalBytes && !provider.Capabilities().Remote {
			continue
		}
		name := provider.Name()
		job.AttemptedProviders = append(job.AttemptedProviders, name)
		res, err := provider.Edit(ctx, req)
		if err != nil {
			lastErr = err
			w.recordMetric(req.Action, name, "failed", w.now().Sub(start), req.SourceBytes, 0)
			continue
		}
		job.Provider = name
		job.OutputURI = res.OutputURI
		job.OutputContentType = res.OutputContentType
		job.OutputBytes = res.OutputBytes
		job.ProviderRequestID = res.ProviderRequestID
		job.UpdatedAt = w.now().UTC()
		w.recordMetric(req.Action, name, "ok", w.now().Sub(start), req.SourceBytes, res.OutputBytes)
		return w.saveJob(job), nil
	}
	if len(job.AttemptedProviders) == 0 {
		return ImageEditJob{}, fmt.Errorf("%w: large asset requires remote-capable provider", ErrImageEditNoProvider)
	}
	return ImageEditJob{}, fmt.Errorf("%w: attempts=%v: %w", ErrImageEditFailed, job.AttemptedProviders, lastErr)
}

func (w *ImageEditWorkflow) orderedProviders(req ImageEditRequest) []ImageEditProvider {
	if len(req.PreferredProviders) == 0 {
		return append([]ImageEditProvider(nil), w.providers...)
	}
	byName := make(map[string]ImageEditProvider, len(w.providers))
	for _, provider := range w.providers {
		byName[provider.Name()] = provider
	}
	out := make([]ImageEditProvider, 0, len(w.providers))
	used := map[string]struct{}{}
	for _, preferred := range req.PreferredProviders {
		name := strings.TrimSpace(preferred)
		if provider, ok := byName[name]; ok {
			out = append(out, provider)
			used[name] = struct{}{}
		}
	}
	for _, provider := range w.providers {
		if _, ok := used[provider.Name()]; !ok {
			out = append(out, provider)
		}
	}
	return out
}

func (w *ImageEditWorkflow) saveJob(job ImageEditJob) ImageEditJob {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.jobs[job.ID] = job
	return job
}

func (w *ImageEditWorkflow) loadJob(id string) (ImageEditJob, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	job, ok := w.jobs[strings.TrimSpace(id)]
	if !ok {
		return ImageEditJob{}, fmt.Errorf("%w: %q", ErrImageEditNotFound, id)
	}
	return job, nil
}

func (w *ImageEditWorkflow) recordMetric(action ImageEditAction, provider, status string, duration time.Duration, sourceBytes int64, outputBytes int) {
	if w.imageEditMetrics == nil {
		return
	}
	w.imageEditMetrics(ImageEditMetric{
		Action:      action,
		Provider:    provider,
		Status:      status,
		Duration:    duration,
		SourceBytes: sourceBytes,
		OutputBytes: outputBytes,
	})
}

func validateImageEditRequest(req ImageEditRequest) error {
	if strings.TrimSpace(req.RequestID) == "" {
		return fmt.Errorf("%w: RequestID required", ErrImageEditInvalid)
	}
	if strings.TrimSpace(req.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrImageEditInvalid)
	}
	if strings.TrimSpace(req.ProductID) == "" {
		return fmt.Errorf("%w: ProductID required", ErrImageEditInvalid)
	}
	if strings.TrimSpace(req.SourceURI) == "" {
		return fmt.Errorf("%w: SourceURI required", ErrImageEditInvalid)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return fmt.Errorf("%w: Prompt required", ErrImageEditInvalid)
	}
	if req.Action == "" {
		return fmt.Errorf("%w: Action required", ErrImageEditInvalid)
	}
	if req.SourceBytes < 0 {
		return fmt.Errorf("%w: SourceBytes must be non-negative", ErrImageEditInvalid)
	}
	return nil
}

func imageEditJobFromRequest(req ImageEditRequest, now time.Time) ImageEditJob {
	return ImageEditJob{
		ID:          strings.TrimSpace(req.RequestID),
		TenantID:    strings.TrimSpace(req.TenantID),
		ProductID:   strings.TrimSpace(req.ProductID),
		Action:      req.Action,
		SourceURI:   strings.TrimSpace(req.SourceURI),
		SourceBytes: req.SourceBytes,
		Prompt:      strings.TrimSpace(req.Prompt),
		CreatedAt:   now.UTC(),
		UpdatedAt:   now.UTC(),
	}
}

func requestFromImageEditJob(job ImageEditJob) ImageEditRequest {
	return ImageEditRequest{
		RequestID:   job.ID,
		TenantID:    job.TenantID,
		ProductID:   job.ProductID,
		SourceURI:   job.SourceURI,
		Prompt:      job.Prompt,
		Action:      job.Action,
		SourceBytes: job.SourceBytes,
		AutoApprove: true,
	}
}

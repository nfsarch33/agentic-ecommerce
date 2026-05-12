package media

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestImageEditWorkflow_RejectsInvalidProviderNeutralRequest(t *testing.T) {
	t.Parallel()

	workflow, err := NewImageEditWorkflow(ImageEditWorkflowConfig{
		Providers: []ImageEditProvider{newFakeImageEditProvider("fleet-image-bridge", true, nil)},
	})
	if err != nil {
		t.Fatalf("NewImageEditWorkflow: %v", err)
	}

	_, err = workflow.Request(context.Background(), ImageEditRequest{
		RequestID:   "edit-invalid",
		TenantID:    "tenant-1",
		ProductID:   "product-1",
		SourceURI:   "oci://bucket/source.png",
		Prompt:      "   ",
		Action:      ImageEditActionLifestyleGeneration,
		SourceBytes: 42,
		AutoApprove: true,
	})
	if !errors.Is(err, ErrImageEditInvalid) {
		t.Fatalf("Request err = %v, want ErrImageEditInvalid", err)
	}
}

func TestImageEditWorkflow_LargeAssetsRouteToRemoteProvider(t *testing.T) {
	t.Parallel()

	local := newFakeImageEditProvider("local-stub", false, nil)
	remote := newFakeImageEditProvider("fleet-image-bridge", true, nil)
	workflow, err := NewImageEditWorkflow(ImageEditWorkflowConfig{
		Providers: []ImageEditProvider{local, remote},
		Now:       fixedImageEditNow,
	})
	if err != nil {
		t.Fatalf("NewImageEditWorkflow: %v", err)
	}

	job, err := workflow.Request(context.Background(), validImageEditRequest(ImageEditRequest{
		RequestID:   "edit-large",
		SourceBytes: MaxLocalDecodeBytes + 1,
		AutoApprove: true,
	}))
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if local.calls != 0 {
		t.Fatalf("local provider calls = %d, want 0 for large remote-routed asset", local.calls)
	}
	if remote.calls != 1 {
		t.Fatalf("remote provider calls = %d, want 1", remote.calls)
	}
	if job.Provider != "fleet-image-bridge" {
		t.Fatalf("Provider = %q, want fleet-image-bridge", job.Provider)
	}
	if job.ApprovalState != ImageEditApprovalApproved {
		t.Fatalf("ApprovalState = %q, want %q", job.ApprovalState, ImageEditApprovalApproved)
	}
	if job.OutputURI == "" {
		t.Fatal("OutputURI should be populated after approved execution")
	}
}

func TestImageEditWorkflow_ApprovalStatesGateProviderExecution(t *testing.T) {
	t.Parallel()

	provider := newFakeImageEditProvider("openai-image-bridge", true, nil)
	workflow, err := NewImageEditWorkflow(ImageEditWorkflowConfig{
		Providers: []ImageEditProvider{provider},
		Now:       fixedImageEditNow,
	})
	if err != nil {
		t.Fatalf("NewImageEditWorkflow: %v", err)
	}

	pending, err := workflow.Request(context.Background(), validImageEditRequest(ImageEditRequest{
		RequestID:        "edit-needs-review",
		RequiresApproval: true,
	}))
	if err != nil {
		t.Fatalf("Request pending: %v", err)
	}
	if pending.ApprovalState != ImageEditApprovalPending {
		t.Fatalf("ApprovalState = %q, want %q", pending.ApprovalState, ImageEditApprovalPending)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls before approval = %d, want 0", provider.calls)
	}

	approved, err := workflow.Approve(context.Background(), pending.ID)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.ApprovalState != ImageEditApprovalApproved {
		t.Fatalf("ApprovalState = %q, want %q", approved.ApprovalState, ImageEditApprovalApproved)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls after approval = %d, want 1", provider.calls)
	}

	next, err := workflow.Request(context.Background(), validImageEditRequest(ImageEditRequest{
		RequestID:        "edit-reject",
		RequiresApproval: true,
	}))
	if err != nil {
		t.Fatalf("Request reject: %v", err)
	}
	rejected, err := workflow.Reject(context.Background(), next.ID, "brand mismatch")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if rejected.ApprovalState != ImageEditApprovalRejected {
		t.Fatalf("ApprovalState = %q, want %q", rejected.ApprovalState, ImageEditApprovalRejected)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls after reject = %d, want still 1", provider.calls)
	}
}

func TestImageEditWorkflow_FallbackHonorsConfiguredProviderOrder(t *testing.T) {
	t.Parallel()

	first := newFakeImageEditProvider("openai-image-bridge", true, errors.New("quota exhausted"))
	second := newFakeImageEditProvider("minimax-image-bridge", true, nil)
	workflow, err := NewImageEditWorkflow(ImageEditWorkflowConfig{
		Providers: []ImageEditProvider{first, second},
		Now:       fixedImageEditNow,
	})
	if err != nil {
		t.Fatalf("NewImageEditWorkflow: %v", err)
	}

	job, err := workflow.Request(context.Background(), validImageEditRequest(ImageEditRequest{
		RequestID:   "edit-fallback",
		AutoApprove: true,
	}))
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("provider calls = first:%d second:%d, want 1/1", first.calls, second.calls)
	}
	if job.Provider != "minimax-image-bridge" {
		t.Fatalf("Provider = %q, want minimax-image-bridge", job.Provider)
	}
	wantAttempts := []string{"openai-image-bridge", "minimax-image-bridge"}
	if !reflect.DeepEqual(job.AttemptedProviders, wantAttempts) {
		t.Fatalf("AttemptedProviders = %#v, want %#v", job.AttemptedProviders, wantAttempts)
	}
}

func validImageEditRequest(overrides ImageEditRequest) ImageEditRequest {
	req := ImageEditRequest{
		RequestID:   "edit-1",
		TenantID:    "tenant-1",
		ProductID:   "product-1",
		SourceURI:   "oci://media/products/product-1/source.png",
		Prompt:      "Create a clean marketplace hero image on a white background.",
		Action:      ImageEditActionLifestyleGeneration,
		SourceBytes: 128 * 1024,
	}
	if overrides.RequestID != "" {
		req.RequestID = overrides.RequestID
	}
	if overrides.TenantID != "" {
		req.TenantID = overrides.TenantID
	}
	if overrides.ProductID != "" {
		req.ProductID = overrides.ProductID
	}
	if overrides.SourceURI != "" {
		req.SourceURI = overrides.SourceURI
	}
	if overrides.Prompt != "" {
		req.Prompt = overrides.Prompt
	}
	if overrides.Action != "" {
		req.Action = overrides.Action
	}
	if overrides.SourceBytes != 0 {
		req.SourceBytes = overrides.SourceBytes
	}
	req.RequiresApproval = overrides.RequiresApproval
	req.AutoApprove = overrides.AutoApprove
	req.PreferredProviders = overrides.PreferredProviders
	return req
}

func fixedImageEditNow() time.Time {
	return time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
}

type fakeImageEditProvider struct {
	name   string
	remote bool
	err    error
	calls  int
}

func newFakeImageEditProvider(name string, remote bool, err error) *fakeImageEditProvider {
	return &fakeImageEditProvider{name: name, remote: remote, err: err}
}

func (p *fakeImageEditProvider) Name() string { return p.name }

func (p *fakeImageEditProvider) Capabilities() ImageEditProviderCapabilities {
	return ImageEditProviderCapabilities{Remote: p.remote}
}

func (p *fakeImageEditProvider) Edit(_ context.Context, req ImageEditRequest) (ImageEditProviderResult, error) {
	p.calls++
	if p.err != nil {
		return ImageEditProviderResult{}, p.err
	}
	return ImageEditProviderResult{
		OutputURI:          "oci://media/edited/" + req.RequestID + ".png",
		OutputContentType:  "image/png",
		OutputBytes:        int(req.SourceBytes / 2),
		ProviderRequestID:  p.name + "-request-" + req.RequestID,
		ProviderDiagnostic: "fake-provider",
	}, nil
}

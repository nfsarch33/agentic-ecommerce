package media

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestImageEditWorkflow_QALargeAssetWithoutRemoteProviderFailsBeforeLocalCall(t *testing.T) {
	t.Parallel()

	local := newFakeImageEditProvider("local-stub", false, nil)
	workflow, err := NewImageEditWorkflow(ImageEditWorkflowConfig{
		Providers: []ImageEditProvider{local},
		Now:       fixedImageEditNow,
	})
	if err != nil {
		t.Fatalf("NewImageEditWorkflow: %v", err)
	}

	_, err = workflow.Request(context.Background(), validImageEditRequest(ImageEditRequest{
		RequestID:   "edit-large-local-only",
		SourceBytes: MaxLocalDecodeBytes + 1,
		AutoApprove: true,
	}))
	if !errors.Is(err, ErrImageEditNoProvider) {
		t.Fatalf("Request err = %v, want ErrImageEditNoProvider", err)
	}
	if local.calls != 0 {
		t.Fatalf("local provider calls = %d, want 0 when memory ceiling requires remote routing", local.calls)
	}
}

func TestImageEditWorkflow_QAPreferredProviderOrderFallsBackToConfiguredOrder(t *testing.T) {
	t.Parallel()

	openai := newFakeImageEditProvider("openai-image-bridge", true, nil)
	minimax := newFakeImageEditProvider("minimax-image-bridge", true, errors.New("temporary quota"))
	fleet := newFakeImageEditProvider("fleet-image-bridge", true, nil)
	workflow, err := NewImageEditWorkflow(ImageEditWorkflowConfig{
		Providers: []ImageEditProvider{openai, minimax, fleet},
		Now:       fixedImageEditNow,
	})
	if err != nil {
		t.Fatalf("NewImageEditWorkflow: %v", err)
	}

	job, err := workflow.Request(context.Background(), validImageEditRequest(ImageEditRequest{
		RequestID:          "edit-preferred-fallback",
		AutoApprove:        true,
		PreferredProviders: []string{"unknown-provider", "minimax-image-bridge"},
	}))
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if job.Provider != "openai-image-bridge" {
		t.Fatalf("Provider = %q, want openai-image-bridge after preferred provider fails", job.Provider)
	}
	wantAttempts := []string{"minimax-image-bridge", "openai-image-bridge"}
	if !reflect.DeepEqual(job.AttemptedProviders, wantAttempts) {
		t.Fatalf("AttemptedProviders = %#v, want %#v", job.AttemptedProviders, wantAttempts)
	}
	if minimax.calls != 1 || openai.calls != 1 || fleet.calls != 0 {
		t.Fatalf("provider calls = minimax:%d openai:%d fleet:%d, want 1/1/0", minimax.calls, openai.calls, fleet.calls)
	}
}

func TestImageEditWorkflow_QAMetricsExposeEvoMapMediaKPISample(t *testing.T) {
	t.Parallel()

	var metrics []ImageEditMetric
	first := newFakeImageEditProvider("openai-image-bridge", true, errors.New("quota exhausted"))
	second := newFakeImageEditProvider("fleet-image-bridge", true, nil)
	workflow, err := NewImageEditWorkflow(ImageEditWorkflowConfig{
		Providers: []ImageEditProvider{first, second},
		Now:       fixedImageEditNow,
		ImageEditMetrics: func(sample ImageEditMetric) {
			metrics = append(metrics, sample)
		},
	})
	if err != nil {
		t.Fatalf("NewImageEditWorkflow: %v", err)
	}

	job, err := workflow.Request(context.Background(), validImageEditRequest(ImageEditRequest{
		RequestID:   "edit-kpi",
		SourceBytes: 256 * 1024,
		AutoApprove: true,
	}))
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if job.Provider != "fleet-image-bridge" {
		t.Fatalf("Provider = %q, want fleet-image-bridge", job.Provider)
	}
	if len(metrics) != 2 {
		t.Fatalf("metrics len = %d, want 2", len(metrics))
	}
	if metrics[0].Status != "failed" || metrics[0].Provider != "openai-image-bridge" {
		t.Fatalf("first metric = %#v, want failed openai-image-bridge", metrics[0])
	}
	kpi := metrics[1].MediaKPISample()
	if kpi.TenantID != "tenant-1" || kpi.ProductID != "product-1" {
		t.Fatalf("KPI tenant/product = %q/%q, want tenant-1/product-1", kpi.TenantID, kpi.ProductID)
	}
	if kpi.Provider != "fleet-image-bridge" || kpi.Status != "ok" {
		t.Fatalf("KPI provider/status = %q/%q, want fleet-image-bridge/ok", kpi.Provider, kpi.Status)
	}
	if kpi.Action != string(ImageEditActionLifestyleGeneration) {
		t.Fatalf("KPI action = %q, want %q", kpi.Action, ImageEditActionLifestyleGeneration)
	}
	if kpi.SourceBytes != 256*1024 || kpi.OutputBytes == 0 {
		t.Fatalf("KPI bytes = source:%d output:%d, want source 262144 and output > 0", kpi.SourceBytes, kpi.OutputBytes)
	}
}

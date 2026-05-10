package temporal

import (
	"testing"

	"go.temporal.io/sdk/interceptor"
)

func TestTemporalInterceptor_ReturnsNonNil(t *testing.T) {
	t.Parallel()
	got := TemporalInterceptor()
	if got == nil {
		t.Fatal("TemporalInterceptor() returned nil")
	}
}

func TestTemporalInterceptor_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ interceptor.WorkerInterceptor = TemporalInterceptor()
}

func TestActivityTypeName_NilInput(t *testing.T) {
	t.Parallel()
	got := activityTypeName(nil)
	if got != "unknown" {
		t.Fatalf("activityTypeName(nil) = %q, want unknown", got)
	}
}

func TestActivityTypeName_EmptyArgs(t *testing.T) {
	t.Parallel()
	got := activityTypeName(&interceptor.ExecuteActivityInput{})
	if got != "unknown" {
		t.Fatalf("activityTypeName(empty) = %q, want unknown", got)
	}
}

func TestActivityTypeName_StringArg(t *testing.T) {
	t.Parallel()
	got := activityTypeName(&interceptor.ExecuteActivityInput{
		Args: []interface{}{"content_generation.generate"},
	})
	if got != "content_generation.generate" {
		t.Fatalf("activityTypeName = %q, want content_generation.generate", got)
	}
}

func TestActivityTypeName_NonStringArg(t *testing.T) {
	t.Parallel()
	got := activityTypeName(&interceptor.ExecuteActivityInput{
		Args: []interface{}{42},
	})
	if got != "unknown" {
		t.Fatalf("activityTypeName(int) = %q, want unknown", got)
	}
}

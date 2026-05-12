package temporal

import (
	"context"
	"errors"
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

func TestActivityInterceptorExecuteActivityUsesUnknownForEmptyArgs(t *testing.T) {
	t.Parallel()
	next := &activityNext{result: "ok"}
	wrapped := TemporalInterceptor().InterceptActivity(context.Background(), next)

	got, err := wrapped.ExecuteActivity(context.Background(), &interceptor.ExecuteActivityInput{})

	if err != nil {
		t.Fatalf("ExecuteActivity returned error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("ExecuteActivity result = %v, want ok", got)
	}
	if !next.called {
		t.Fatal("next activity interceptor was not called")
	}
}

func TestActivityInterceptorExecuteActivityPropagatesNextErrorForNonStringArgs(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("activity failed")
	next := &activityNext{err: wantErr}
	wrapped := TemporalInterceptor().InterceptActivity(context.Background(), next)

	got, err := wrapped.ExecuteActivity(context.Background(), &interceptor.ExecuteActivityInput{
		Args: []interface{}{42},
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("ExecuteActivity error = %v, want %v", err, wantErr)
	}
	if got != nil {
		t.Fatalf("ExecuteActivity result = %v, want nil", got)
	}
	if !next.called {
		t.Fatal("next activity interceptor was not called")
	}
}

type activityNext struct {
	interceptor.ActivityInboundInterceptorBase
	result interface{}
	err    error
	called bool
}

func (n *activityNext) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	n.called = true
	return n.result, n.err
}

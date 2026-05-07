package tenant

import (
	"context"
	"testing"
)

func TestWithID_RoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = WithID(ctx, "shop-abc")

	got := FromContext(ctx)
	if got != "shop-abc" {
		t.Errorf("FromContext = %q, want %q", got, "shop-abc")
	}
}

func TestFromContext_EmptyReturnsDefault(t *testing.T) {
	ctx := context.Background()
	got := FromContext(ctx)
	if got != Default {
		t.Errorf("FromContext empty = %q, want %q", got, Default)
	}
}

func TestFromContext_ExplicitDefault(t *testing.T) {
	ctx := WithID(context.Background(), Default)
	got := FromContext(ctx)
	if got != Default {
		t.Errorf("FromContext explicit default = %q, want %q", got, Default)
	}
}

func TestFromContext_EmptyString(t *testing.T) {
	ctx := WithID(context.Background(), "")
	got := FromContext(ctx)
	if got != Default {
		t.Errorf("FromContext empty string = %q, want %q", got, Default)
	}
}

func TestID_StringConversion(t *testing.T) {
	id := ID("tenant-123")
	if string(id) != "tenant-123" {
		t.Errorf("string(ID) = %q, want %q", string(id), "tenant-123")
	}
}

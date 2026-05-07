package catalog

import (
	"errors"
	"testing"
)

func TestParseProductStatus_ValidStatuses(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"draft", "active", "archived"} {
		got, err := ParseProductStatus(s)
		if err != nil {
			t.Fatalf("ParseProductStatus(%q): %v", s, err)
		}
		if got.String() != s {
			t.Fatalf("String() = %q, want %q", got.String(), s)
		}
	}
}

func TestParseProductStatus_RejectsInvalid(t *testing.T) {
	t.Parallel()

	_, err := ParseProductStatus("deleted")
	if !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
}

func TestProductStatus_IsPublishable(t *testing.T) {
	t.Parallel()

	if !StatusActive.IsPublishable() {
		t.Fatal("active should be publishable")
	}
	if StatusDraft.IsPublishable() {
		t.Fatal("draft should not be publishable")
	}
	if StatusArchived.IsPublishable() {
		t.Fatal("archived should not be publishable")
	}
}

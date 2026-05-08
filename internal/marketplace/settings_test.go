package marketplace

import "testing"

func TestSettingsStoreCopyOnRead(t *testing.T) {
	t.Parallel()
	store := newSettingsStore()
	store.set("tenant-a", "stripe", map[string]any{"webhook": "https://example.com"})
	got := store.get("tenant-a", "stripe")
	got["webhook"] = "mutated"
	again := store.get("tenant-a", "stripe")
	if again["webhook"] == "mutated" {
		t.Fatalf("settings store should defensive-copy on get; got %v", again)
	}
}

func TestSettingsStoreCopyOnWrite(t *testing.T) {
	t.Parallel()
	store := newSettingsStore()
	values := map[string]any{"webhook": "https://example.com"}
	store.set("tenant-a", "stripe", values)
	values["webhook"] = "mutated"
	got := store.get("tenant-a", "stripe")
	if got["webhook"] == "mutated" {
		t.Fatalf("settings store should defensive-copy on set; got %v", got)
	}
}

func TestSettingsStoreNilDeletes(t *testing.T) {
	t.Parallel()
	store := newSettingsStore()
	store.set("tenant-a", "stripe", map[string]any{"k": "v"})
	store.set("tenant-a", "stripe", nil)
	if got := store.get("tenant-a", "stripe"); len(got) != 0 {
		t.Fatalf("nil values should remove the entry; got %v", got)
	}
}

func TestSettingsStoreUnknownReturnsEmpty(t *testing.T) {
	t.Parallel()
	store := newSettingsStore()
	got := store.get("tenant-a", "unknown")
	if got == nil {
		t.Fatalf("unknown lookup should return non-nil empty map")
	}
	if len(got) != 0 {
		t.Fatalf("unknown lookup should be empty; got %v", got)
	}
}

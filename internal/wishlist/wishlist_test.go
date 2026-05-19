package wishlist_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/wishlist"
)

func TestService_Create(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()
	wl := svc.Create("owner-1")
	if wl == nil {
		t.Fatal("expected non-nil wishlist")
	}
	if wl.OwnerID != "owner-1" {
		t.Errorf("want owner-1, got %s", wl.OwnerID)
	}
	if wl.ID == "" {
		t.Error("want non-empty ID")
	}
}

func TestService_Get(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()
	wl := svc.Create("owner-2")

	got, err := svc.Get(wl.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != wl.ID {
		t.Errorf("want %s, got %s", wl.ID, got.ID)
	}
}

func TestService_Get_NotFound(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()
	_, err := svc.Get("missing")
	if err != wishlist.ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestService_AddItem(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()
	wl := svc.Create("owner-3")

	if err := svc.AddItem(wl.ID, "prod-1", 49.99, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := svc.Get(wl.ID)
	item, ok := got.Items["prod-1"]
	if !ok {
		t.Fatal("want prod-1 in items")
	}
	if item.PriceAtAdd != 49.99 {
		t.Errorf("want price 49.99, got %f", item.PriceAtAdd)
	}
	if !item.NotifyOnDrop {
		t.Error("want NotifyOnDrop=true")
	}
}

func TestService_AddItem_NotFound(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()
	if err := svc.AddItem("missing", "prod-1", 10, false); err != wishlist.ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestService_RemoveItem(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()
	wl := svc.Create("owner-4")
	svc.AddItem(wl.ID, "prod-2", 20.0, false)

	if err := svc.RemoveItem(wl.ID, "prod-2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := svc.Get(wl.ID)
	if _, ok := got.Items["prod-2"]; ok {
		t.Error("want prod-2 removed")
	}
}

func TestService_RemoveItem_NotFound(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()
	wl := svc.Create("owner-5")
	if err := svc.RemoveItem(wl.ID, "missing-prod"); err != wishlist.ErrItemNotFound {
		t.Errorf("want ErrItemNotFound, got %v", err)
	}
}

func TestService_RemoveItem_WishlistNotFound(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()
	if err := svc.RemoveItem("no-wl", "prod-x"); err != wishlist.ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestService_GenerateShareToken(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()
	wl := svc.Create("owner-6")

	token1 := svc.GenerateShareToken(wl.ID)
	if token1 == "" {
		t.Error("want non-empty token")
	}

	// Second call should produce a different token
	token2 := svc.GenerateShareToken(wl.ID)
	if token1 == token2 {
		t.Error("want different tokens on successive calls")
	}

	got, _ := svc.Get(wl.ID)
	if got.ShareToken != token2 {
		t.Errorf("want token stored, got %s", got.ShareToken)
	}
}

func TestService_GenerateShareToken_Missing(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()
	token := svc.GenerateShareToken("no-id")
	if token != "" {
		t.Errorf("want empty string for missing wishlist, got %s", token)
	}
}

// PriceDropDetector tests

func TestPriceDropDetector_Detects(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()
	wl := svc.Create("owner-7")
	svc.AddItem(wl.ID, "prod-drop", 100.0, true)
	svc.AddItem(wl.ID, "prod-stable", 50.0, true)
	svc.AddItem(wl.ID, "prod-no-notify", 80.0, false)

	wlFull, _ := svc.Get(wl.ID)
	det := wishlist.PriceDropDetector{}
	drops := det.Check(wlFull, map[string]float64{
		"prod-drop":      80.0, // dropped
		"prod-stable":    50.0, // same
		"prod-no-notify": 60.0, // dropped but no notify
	})

	if len(drops) != 1 {
		t.Fatalf("want 1 drop, got %d: %v", len(drops), drops)
	}
	d := drops[0]
	if d.ProductID != "prod-drop" {
		t.Errorf("want prod-drop, got %s", d.ProductID)
	}
	if d.OldPrice != 100.0 || d.NewPrice != 80.0 {
		t.Errorf("unexpected prices: %+v", d)
	}
	wantPct := 20.0
	if abs(d.DropPct-wantPct) > 0.001 {
		t.Errorf("want drop pct ~20.0, got %f", d.DropPct)
	}
}

func TestPriceDropDetector_NoDrop(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()
	wl := svc.Create("owner-8")
	svc.AddItem(wl.ID, "prod-up", 50.0, true)

	wlFull, _ := svc.Get(wl.ID)
	det := wishlist.PriceDropDetector{}
	drops := det.Check(wlFull, map[string]float64{"prod-up": 60.0})
	if len(drops) != 0 {
		t.Errorf("want 0 drops, got %d", len(drops))
	}
}

// Analytics tests

func TestMostWishlisted(t *testing.T) {
	t.Parallel()
	svc := wishlist.NewService()

	wl1 := svc.Create("u1")
	wl2 := svc.Create("u2")
	wl3 := svc.Create("u3")

	svc.AddItem(wl1.ID, "popular", 10, false)
	svc.AddItem(wl2.ID, "popular", 10, false)
	svc.AddItem(wl3.ID, "popular", 10, false)
	svc.AddItem(wl1.ID, "rare", 20, false)

	counts := wishlist.MostWishlisted(svc)
	if len(counts) < 2 {
		t.Fatalf("want at least 2 entries, got %d", len(counts))
	}
	if counts[0].ProductID != "popular" {
		t.Errorf("want popular first, got %s", counts[0].ProductID)
	}
	if counts[0].Count != 3 {
		t.Errorf("want count 3, got %d", counts[0].Count)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

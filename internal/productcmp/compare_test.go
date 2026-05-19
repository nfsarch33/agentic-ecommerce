package productcmp_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/productcmp"
)

func attr(name, value, typ string) productcmp.Attribute {
	return productcmp.Attribute{Name: name, Value: value, Type: typ}
}

func makeProduct(id string, attrs ...productcmp.Attribute) productcmp.Product {
	return productcmp.Product{ID: id, Name: "Product " + id, Price: 10.0, Attributes: attrs}
}

// Matrix tests

func TestMatrix_AddAndProducts(t *testing.T) {
	t.Parallel()
	m := productcmp.NewMatrix()
	p1 := makeProduct("p1", attr("color", "red", productcmp.TypeString))
	p2 := makeProduct("p2", attr("color", "blue", productcmp.TypeString))

	if err := m.Add(p1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := m.Add(p2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prods := m.Products()
	if len(prods) != 2 {
		t.Errorf("want 2 products, got %d", len(prods))
	}
}

func TestMatrix_Remove(t *testing.T) {
	t.Parallel()
	m := productcmp.NewMatrix()
	m.Add(makeProduct("r1"))
	m.Add(makeProduct("r2"))
	m.Remove("r1")
	prods := m.Products()
	if len(prods) != 1 || prods[0].ID != "r2" {
		t.Errorf("want [r2], got %v", prods)
	}
}

func TestMatrix_MaxCompareLimit(t *testing.T) {
	t.Parallel()
	m := productcmp.NewMatrix()
	m.MaxCompare = 2
	if err := m.Add(makeProduct("m1")); err != nil {
		t.Fatalf("unexpected error on first add: %v", err)
	}
	if err := m.Add(makeProduct("m2")); err != nil {
		t.Fatalf("unexpected error on second add: %v", err)
	}
	if err := m.Add(makeProduct("m3")); err != productcmp.ErrMaxExceeded {
		t.Errorf("want ErrMaxExceeded, got %v", err)
	}
	// Should still have 2 products
	if got := len(m.Products()); got != 2 {
		t.Errorf("want 2 products, got %d", got)
	}
}

func TestMatrix_UpdateExisting(t *testing.T) {
	t.Parallel()
	m := productcmp.NewMatrix()
	m.MaxCompare = 2
	m.Add(makeProduct("u1"))
	m.Add(makeProduct("u2"))
	// Update existing — should not trigger limit error
	updated := makeProduct("u1", attr("weight", "1kg", productcmp.TypeString))
	if err := m.Add(updated); err != nil {
		t.Fatalf("updating existing product should succeed: %v", err)
	}
	prods := m.Products()
	if len(prods) != 2 {
		t.Errorf("want 2 products after update, got %d", len(prods))
	}
}

// Compare tests

func TestCompare_SharedAttributes(t *testing.T) {
	t.Parallel()
	p1 := makeProduct("c1",
		attr("brand", "Acme", productcmp.TypeString),
		attr("color", "red", productcmp.TypeString),
	)
	p2 := makeProduct("c2",
		attr("brand", "Acme", productcmp.TypeString),
		attr("color", "blue", productcmp.TypeString),
	)

	result := productcmp.Compare([]productcmp.Product{p1, p2})

	if !containsAll(result.SharedAttributes, []string{"brand"}) {
		t.Errorf("want brand in shared, got %v", result.SharedAttributes)
	}
	if !containsAll(result.DifferingAttributes, []string{"color"}) {
		t.Errorf("want color in differing, got %v", result.DifferingAttributes)
	}
}

func TestCompare_AllDifferent(t *testing.T) {
	t.Parallel()
	p1 := makeProduct("d1", attr("size", "S", productcmp.TypeString))
	p2 := makeProduct("d2", attr("size", "L", productcmp.TypeString))

	result := productcmp.Compare([]productcmp.Product{p1, p2})
	if len(result.SharedAttributes) != 0 {
		t.Errorf("want no shared attributes, got %v", result.SharedAttributes)
	}
	if !containsAll(result.DifferingAttributes, []string{"size"}) {
		t.Errorf("want size in differing, got %v", result.DifferingAttributes)
	}
}

func TestCompare_Empty(t *testing.T) {
	t.Parallel()
	result := productcmp.Compare(nil)
	if len(result.Products) != 0 {
		t.Error("want empty result for nil input")
	}
}

// Recommender tests

func TestRecommender_Score_FullOverlap(t *testing.T) {
	t.Parallel()
	rec := productcmp.Recommender{}
	ref := makeProduct("ref",
		attr("color", "red", productcmp.TypeString),
		attr("size", "M", productcmp.TypeString),
	)
	cand := makeProduct("cand",
		attr("color", "blue", productcmp.TypeString),
		attr("size", "L", productcmp.TypeString),
	)
	score := rec.Score(cand, ref)
	if score != 1.0 {
		t.Errorf("want 1.0 for same attribute names, got %f", score)
	}
}

func TestRecommender_Score_NoOverlap(t *testing.T) {
	t.Parallel()
	rec := productcmp.Recommender{}
	ref := makeProduct("ref", attr("color", "red", productcmp.TypeString))
	cand := makeProduct("cand", attr("weight", "1kg", productcmp.TypeString))
	score := rec.Score(cand, ref)
	if score != 0.0 {
		t.Errorf("want 0.0 for no overlap, got %f", score)
	}
}

func TestRecommender_Score_PartialOverlap(t *testing.T) {
	t.Parallel()
	rec := productcmp.Recommender{}
	ref := makeProduct("ref",
		attr("color", "red", productcmp.TypeString),
		attr("size", "M", productcmp.TypeString),
	)
	cand := makeProduct("cand",
		attr("color", "blue", productcmp.TypeString),
		attr("brand", "Acme", productcmp.TypeString),
	)
	// intersection=1 (color), union=3 (color, size, brand) => 1/3
	score := rec.Score(cand, ref)
	const want = 1.0 / 3.0
	if abs(score-want) > 1e-9 {
		t.Errorf("want ~0.333, got %f", score)
	}
}

func TestRecommender_BestMatch(t *testing.T) {
	t.Parallel()
	rec := productcmp.Recommender{}
	ref := makeProduct("ref",
		attr("color", "red", productcmp.TypeString),
		attr("size", "M", productcmp.TypeString),
		attr("brand", "Acme", productcmp.TypeString),
	)
	low := makeProduct("low", attr("weight", "1kg", productcmp.TypeString))
	high := makeProduct("high",
		attr("color", "blue", productcmp.TypeString),
		attr("size", "L", productcmp.TypeString),
		attr("brand", "Acme", productcmp.TypeString),
	)

	best := rec.BestMatch([]productcmp.Product{low, high}, ref)
	if best == nil || best.ID != "high" {
		t.Errorf("want high as best match, got %v", best)
	}
}

func TestRecommender_BestMatch_Empty(t *testing.T) {
	t.Parallel()
	rec := productcmp.Recommender{}
	ref := makeProduct("ref", attr("color", "red", productcmp.TypeString))
	best := rec.BestMatch(nil, ref)
	if best != nil {
		t.Errorf("want nil for empty candidates, got %v", best)
	}
}

func TestRecommender_Score_BothEmpty(t *testing.T) {
	t.Parallel()
	rec := productcmp.Recommender{}
	ref := makeProduct("ref")
	cand := makeProduct("cand")
	score := rec.Score(cand, ref)
	if score != 0.0 {
		t.Errorf("want 0.0 for both empty, got %f", score)
	}
}

func containsAll(slice, targets []string) bool {
	set := make(map[string]bool)
	for _, s := range slice {
		set[s] = true
	}
	for _, t := range targets {
		if !set[t] {
			return false
		}
	}
	return true
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

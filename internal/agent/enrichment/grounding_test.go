package enrichment

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type plantedProduct struct {
	SKU        string              `json:"sku"`
	Name       string              `json:"name"`
	Price      string              `json:"price"`
	Currency   string              `json:"currency"`
	Categories []string            `json:"categories"`
	Tags       []string            `json:"tags"`
	Attributes map[string][]string `json:"attributes"`
}

type negativeClaim struct {
	SKU    string `json:"sku"`
	Claim  string `json:"claim"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

type plantedCorpus struct {
	Products       []plantedProduct `json:"products"`
	NegativeClaims []negativeClaim  `json:"negative_claims"`
}

func loadPlanted(t *testing.T) plantedCorpus {
	t.Helper()
	raw, err := os.ReadFile("testdata/planted-products.json")
	if err != nil {
		t.Fatalf("read planted corpus: %v", err)
	}
	var c plantedCorpus
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parse planted corpus: %v", err)
	}
	if len(c.Products) != 10 {
		t.Fatalf("planted corpus = %d products, want 10", len(c.Products))
	}
	if len(c.NegativeClaims) < 7 {
		t.Fatalf("negative claims = %d, want >= 7", len(c.NegativeClaims))
	}
	return c
}

func (p plantedProduct) facts() Facts {
	return Facts{
		SKU:         p.SKU,
		Name:        p.Name,
		Currency:    p.Currency,
		Price:       p.Price,
		Categories:  p.Categories,
		Tags:        p.Tags,
		AttrOptions: p.Attributes,
	}
}

func (p plantedProduct) bySKU(c plantedCorpus) plantedProduct {
	for _, q := range c.Products {
		if q.SKU == p.SKU {
			return q
		}
	}
	return p
}

// The grounding rule, positive half: a draft that cites only what the row
// carries (its own name, its real attribute values, the real price) passes
// with zero violations — across all ten planted products.
func TestGroundedDraftPassesOnAllPlantedProducts(t *testing.T) {
	c := loadPlanted(t)
	for _, p := range c.Products {
		d := Draft{
			Description:     p.Name + " — a dependable pick for everyday use.",
			SEOTitle:        p.Name + " | Store",
			MetaDescription: "Shop the " + p.Name + ". A dependable pick for everyday use.",
			AltText:         p.Name + " product photo on a neutral background, front view",
		}
		if vs := Check(d, p.facts()); len(vs) != 0 {
			t.Errorf("%s: grounded draft flagged: %v", p.SKU, vs)
		}
	}
}

// THE MUTANT-KILLER (the ticket's named mutant): a draft naming an attribute
// the product LACKS must FAIL the grounding check with that claim named.
// Every negative claim in the corpus is planted into a real draft field and
// the check must name it.
//
// MUTANT: make Check return nil (or drop any claim branch) and this test
// goes red — the ungrounded claim is no longer named.
func TestDraftNamingAnAttributeTheProductLacksFails(t *testing.T) {
	c := loadPlanted(t)
	for _, n := range c.NegativeClaims {
		var p plantedProduct
		for _, q := range c.Products {
			if q.SKU == n.SKU {
				p = q
				break
			}
		}
		if p.SKU == "" {
			t.Fatalf("negative claim references unknown sku %s", n.SKU)
		}
		d := Draft{
			Description:     p.Name + " — " + n.Claim + " and a dependable pick for everyday use.",
			SEOTitle:        p.Name + " | Store",
			MetaDescription: "Shop the " + p.Name + ".",
			AltText:         p.Name + " product photo",
		}
		vs := Check(d, p.facts())
		found := false
		norm := func(s string) string { return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "$", "") }
		for _, v := range vs {
			if norm(v.Token) == norm(n.Claim) || strings.Contains(norm(v.Token), norm(n.Claim)) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: claim %q (%s) not named by the check: violations=%v", n.SKU, n.Claim, n.Reason, vs)
		}
	}
}

// A wrong price is the worst hallucination in commerce: the draft's price
// must equal the store's exactly or name none.
func TestWrongPriceFailsMatchingPricePasses(t *testing.T) {
	f := Facts{Name: "Copper Pour-Over Kettle", Price: "89.00", AttrOptions: map[string][]string{"Material": {"Copper"}}}
	wrong := Draft{Description: "The Copper Pour-Over Kettle, now $49.00."}
	if vs := Check(wrong, f); len(vs) == 0 {
		t.Fatal("a wrong price passed the grounding check")
	}
	right := Draft{Description: "The Copper Pour-Over Kettle, $89.00."}
	if vs := Check(right, f); len(vs) != 0 {
		t.Fatalf("the store's own price flagged: %v", vs)
	}
}

// A measure the row does not carry is a violation; the row's own measure
// passes.
func TestMeasureClaimsMatchTheRow(t *testing.T) {
	f := Facts{Name: "Recycled Aluminium Bottle", AttrOptions: map[string][]string{"Capacity": {"750 ml"}}}
	bad := Draft{Description: "Holds 1 L on the trail."}
	if vs := Check(bad, f); len(vs) == 0 {
		t.Fatal("a capacity the row does not offer passed the check")
	}
	good := Draft{Description: "Holds 750 ml on the trail."}
	if vs := Check(good, f); len(vs) != 0 {
		t.Fatalf("the row's own capacity flagged: %v", vs)
	}
}

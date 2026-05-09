// Package compliance is the v3.1.0 EC-1-4 China-import pre-screening
// gate. It evaluates a Product against AU import restricted-list +
// platform (TikTok, Facebook) prohibited-category rules and returns a
// typed Decision the sourcing agent can route on.
//
// Design notes:
//
//   - Pure-function evaluator: no I/O, no goroutines, no clock. Every
//     decision is reproducible from inputs.
//   - Rule data is package-level so reviewers can audit the
//     restricted-category list without indirection. Tenants override
//     via the Settings.Compliance hook (deferred to v3.1.x).
//   - Returned errors wrap ErrRestrictedCategory so callers can use
//     errors.Is.
//   - Tenant-aware: every Decision carries the TenantID it was
//     evaluated under so audit trails stay scoped.
//
// Cite skill: go-clean-architecture (the gate sits in the domain
// layer; the agent adapter consumes Decision via the eventbus).
package compliance

import (
	"errors"
	"fmt"
	"strings"
)

// ErrRestrictedCategory is the sentinel returned when a product fails
// the compliance gate. Callers use errors.Is to branch on it.
var ErrRestrictedCategory = errors.New("compliance: restricted category")

// ErrEmptyProduct is returned when Evaluate is called on a Product
// with no Category or no TenantID. The gate cannot make a safe call
// on an unscoped/unidentified product so this is fatal.
var ErrEmptyProduct = errors.New("compliance: empty product")

// Source identifies the upstream platform a product was sourced from.
// Determines which platform-prohibited category list applies in
// addition to the AU import restrictions.
type Source string

const (
	SourceUnknown  Source = ""
	Source1688     Source = "1688"
	SourceTaobao   Source = "taobao"
	SourceTikTok   Source = "tiktok"
	SourceFacebook Source = "facebook"
)

// Product is the minimal view a compliance evaluator needs. Real
// catalog products carry far more fields; this slim shape lets the
// gate stay decoupled from internal/domain/catalog.Product evolution.
type Product struct {
	ID          string
	TenantID    string
	Title       string
	Category    string
	SubCategory string
	Source      Source
	HSCode      string // optional Harmonised System code; not required
	Restricted  bool   // operator-flagged hint; gate honours it
}

// Decision captures the outcome of a single Evaluate call.
type Decision struct {
	ProductID  string
	TenantID   string
	Pass       bool
	Reasons    []string
	RuleHits   []string
	Source     Source
	Category   string
	BlockedFor []string // platforms the product cannot be listed on
}

// PlatformsBlockedReason returns a human-readable summary of which
// platforms a product cannot ship to. Empty when Pass is true.
func (d Decision) PlatformsBlockedReason() string {
	if d.Pass {
		return ""
	}
	if len(d.BlockedFor) == 0 {
		return strings.Join(d.Reasons, "; ")
	}
	return fmt.Sprintf("blocked: %s", strings.Join(d.BlockedFor, ","))
}

// auImportRestricted is the v3.1.0 baseline AU import restricted list.
// Categories here cannot enter the catalogue regardless of platform.
// Aligned with the Australian Border Force prohibited and restricted
// imports schedule (electronics with Lithium batteries, medical
// devices, firearms, etc.).
var auImportRestricted = map[string]struct{}{
	"firearms":        {},
	"weapons":         {},
	"ammunition":      {},
	"explosives":      {},
	"medical_device":  {},
	"prescription_rx": {},
	"narcotics":       {},
	"asbestos":        {},
	"animal_products": {},
	"endangered":      {},
}

// platformProhibited maps a platform to its additional category
// blocklist. Categories listed here are blocked for that platform
// even if AU import would allow them.
//
// TikTok Shop and Facebook Commerce both ban gambling, dietary
// supplements without registration, vape products, and CBD/THC.
var platformProhibited = map[Source]map[string]struct{}{
	SourceTikTok: {
		"gambling":        {},
		"vape":            {},
		"cbd":             {},
		"thc":             {},
		"crypto":          {},
		"adult":           {},
		"weight_loss":     {},
		"dietary_supp":    {},
		"counterfeit":     {},
		"used_cosmetics":  {},
		"dangerous_goods": {},
	},
	SourceFacebook: {
		"gambling":      {},
		"vape":          {},
		"cbd":           {},
		"thc":           {},
		"crypto":        {},
		"adult":         {},
		"weight_loss":   {},
		"dietary_supp":  {},
		"counterfeit":   {},
		"weapons_parts": {},
	},
}

// Evaluate runs the compliance gate. The Product MUST carry a
// TenantID (tenant-aware audit trail) and a Category (the gate's
// primary discriminator). Returns Decision and either nil (pass) or
// an error wrapping ErrRestrictedCategory (fail).
func Evaluate(p Product) (Decision, error) {
	if p.TenantID == "" || p.Category == "" {
		return Decision{}, fmt.Errorf("%w: tenant_id and category required (id=%q)", ErrEmptyProduct, p.ID)
	}
	d := Decision{
		ProductID: p.ID,
		TenantID:  p.TenantID,
		Source:    p.Source,
		Category:  p.Category,
		Pass:      true,
	}
	cat := normaliseCategory(p.Category)
	subCat := normaliseCategory(p.SubCategory)
	if p.Restricted {
		d.Pass = false
		d.Reasons = append(d.Reasons, "operator-flagged restricted")
		d.RuleHits = append(d.RuleHits, "operator_flag")
		d.BlockedFor = append(d.BlockedFor, "all")
	}
	if matchesAUImportRestriction(cat, subCat) {
		d.Pass = false
		d.Reasons = append(d.Reasons, fmt.Sprintf("category %q is on the AU import restricted list", p.Category))
		d.RuleHits = append(d.RuleHits, "au_import_restricted")
		d.BlockedFor = append(d.BlockedFor, "all")
	}
	for _, platform := range []Source{SourceTikTok, SourceFacebook} {
		if matchesPlatformProhibition(platform, cat, subCat) {
			d.Pass = false
			d.Reasons = append(d.Reasons, fmt.Sprintf("category %q is prohibited on %s", p.Category, platform))
			d.RuleHits = append(d.RuleHits, fmt.Sprintf("platform_prohibited:%s", platform))
			d.BlockedFor = append(d.BlockedFor, string(platform))
		}
	}
	if !d.Pass {
		return d, fmt.Errorf("%w: product %q category %q: %s", ErrRestrictedCategory, p.ID, p.Category, strings.Join(d.Reasons, "; "))
	}
	return d, nil
}

// EvaluateBatch runs Evaluate over a slice and partitions results
// into approved and rejected groups. Errors are NOT returned (the
// Decision carries the failure reason) so callers can drive
// dashboards and metric counters off the partition counts directly.
func EvaluateBatch(products []Product) (approved, rejected []Decision) {
	approved = make([]Decision, 0, len(products))
	rejected = make([]Decision, 0)
	for _, p := range products {
		decision, err := Evaluate(p)
		if err != nil {
			rejected = append(rejected, decision)
			continue
		}
		approved = append(approved, decision)
	}
	return approved, rejected
}

func normaliseCategory(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func matchesAUImportRestriction(cat, subCat string) bool {
	if cat == "" {
		return false
	}
	if _, ok := auImportRestricted[cat]; ok {
		return true
	}
	if subCat != "" {
		if _, ok := auImportRestricted[subCat]; ok {
			return true
		}
	}
	return false
}

func matchesPlatformProhibition(platform Source, cat, subCat string) bool {
	rules, ok := platformProhibited[platform]
	if !ok {
		return false
	}
	if _, hit := rules[cat]; hit {
		return true
	}
	if subCat != "" {
		if _, hit := rules[subCat]; hit {
			return true
		}
	}
	return false
}

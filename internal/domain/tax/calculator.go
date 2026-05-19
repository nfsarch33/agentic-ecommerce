package tax

import "math"

// TaxableItem represents a line item subject to taxation.
type TaxableItem struct {
	SKU    string
	Amount int
	Exempt bool
}

// Jurisdiction identifies the tax authority.
type Jurisdiction struct {
	Country string
	Region  string
}

// TaxLine is a single tax charge in the breakdown.
type TaxLine struct {
	Name   string
	Rate   float64
	Amount int
}

// TaxBreakdown is the result of a tax calculation.
type TaxBreakdown struct {
	Subtotal  int
	TaxAmount int
	Total     int
	TaxRate   float64
	TaxLines  []TaxLine
}

// TaxRule maps a jurisdiction key to one or more rates.
type TaxRule struct {
	Name  string
	Rate  float64
}

// Rules is the full set of tax rules keyed by "COUNTRY" or "COUNTRY/REGION".
type Rules struct {
	rates map[string][]TaxRule
}

// DefaultRules provides common tax rules (AU GST 10%, CA/ON 13%, others 0).
func DefaultRules() Rules {
	return Rules{
		rates: map[string][]TaxRule{
			"AU":    {{Name: "GST", Rate: 0.10}},
			"GB":    {{Name: "VAT", Rate: 0.20}},
			"DE":    {{Name: "MwSt", Rate: 0.19}},
			"CA/ON": {{Name: "HST", Rate: 0.13}},
			"CA/BC": {{Name: "GST", Rate: 0.05}, {Name: "PST", Rate: 0.07}},
			"CA":    {{Name: "GST", Rate: 0.05}},
		},
	}
}

// TaxCalculator applies tax rules to a set of items.
type TaxCalculator struct {
	rules Rules
}

func NewTaxCalculator(rules Rules) *TaxCalculator {
	return &TaxCalculator{rules: rules}
}

// Calculate returns the tax breakdown for the given items and jurisdiction.
func (c *TaxCalculator) Calculate(items []TaxableItem, j Jurisdiction) TaxBreakdown {
	taxableSubtotal := 0
	subtotal := 0
	for _, it := range items {
		subtotal += it.Amount
		if !it.Exempt {
			taxableSubtotal += it.Amount
		}
	}
	rules := c.rulesFor(j)
	var taxLines []TaxLine
	totalTax := 0
	combinedRate := 0.0
	for _, rule := range rules {
		taxAmt := int(math.Round(float64(taxableSubtotal) * rule.Rate))
		taxLines = append(taxLines, TaxLine{Name: rule.Name, Rate: rule.Rate, Amount: taxAmt})
		totalTax += taxAmt
		combinedRate += rule.Rate
	}
	return TaxBreakdown{
		Subtotal:  subtotal,
		TaxAmount: totalTax,
		Total:     subtotal + totalTax,
		TaxRate:   combinedRate,
		TaxLines:  taxLines,
	}
}

func (c *TaxCalculator) rulesFor(j Jurisdiction) []TaxRule {
	// try country/region first
	if j.Region != "" {
		if rules, ok := c.rules.rates[j.Country+"/"+j.Region]; ok {
			return rules
		}
	}
	if rules, ok := c.rules.rates[j.Country]; ok {
		return rules
	}
	return nil
}

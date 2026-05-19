package shipping

import "errors"

// Money holds a minor-unit amount and its currency.
type Money struct {
	Amount   int
	Currency string
}

// Address is the delivery destination.
type Address struct {
	Country    string
	PostalCode string
}

// Cart carries the weight and value of the order for rate calculation.
type Cart struct {
	WeightGrams     int
	OrderValueCents int
}

// ShippingOption is a single rate quote.
type ShippingOption struct {
	Carrier        string
	Method         string
	Cost           Money
	EstimatedDays  int
}

// Rates holds all configuration for the shipping calculator.
type Rates struct {
	FreeThresholdCents int
	// DomesticTiers: keyed by max weight (grams), value is cents
	DomesticTiers []weightTier
	// ZoneRates: map from zone (1-4) to tiers
	ZoneRates map[int][]weightTier
	// CountryZones: country code -> zone
	CountryZones map[string]int
}

type weightTier struct {
	MaxGrams int
	Cents    int
}

// DefaultRates returns sensible default rates.
func DefaultRates() Rates {
	return Rates{
		FreeThresholdCents: 20000,
		DomesticTiers: []weightTier{
			{1000, 799},
			{5000, 1299},
			{20000, 2499},
			{999999, 4999},
		},
		ZoneRates: map[int][]weightTier{
			1: {{1000, 1499}, {5000, 2499}, {20000, 4999}, {999999, 8999}},
			2: {{1000, 1999}, {5000, 3499}, {20000, 6999}, {999999, 12999}},
			3: {{1000, 2999}, {5000, 4999}, {20000, 8999}, {999999, 18999}},
			4: {{1000, 3999}, {5000, 6999}, {20000, 11999}, {999999, 24999}},
		},
		CountryZones: map[string]int{
			"NZ": 1, "SG": 1, "MY": 1,
			"US": 2, "GB": 2, "CA": 2,
			"DE": 3, "FR": 3, "JP": 3,
		},
	}
}

// ShippingCalculator computes shipping options for a cart.
type ShippingCalculator struct {
	rates Rates
}

func NewShippingCalculator(rates Rates) *ShippingCalculator {
	return &ShippingCalculator{rates: rates}
}

// Calculate returns shipping options for the given cart and destination.
func (c *ShippingCalculator) Calculate(cart Cart, dest Address) ([]ShippingOption, error) {
	if cart.WeightGrams <= 0 {
		return nil, errors.New("cart weight must be greater than zero")
	}
	if dest.Country == "AU" {
		return c.domesticOptions(cart), nil
	}
	return c.internationalOptions(cart, dest), nil
}

func (c *ShippingCalculator) domesticOptions(cart Cart) []ShippingOption {
	cost := tieredCost(cart.WeightGrams, c.rates.DomesticTiers)
	var options []ShippingOption
	if c.rates.FreeThresholdCents > 0 && cart.OrderValueCents >= c.rates.FreeThresholdCents {
		options = append(options, ShippingOption{Carrier: "AusPost", Method: "Free Standard", Cost: Money{0, "AUD"}, EstimatedDays: 5})
	}
	options = append(options,
		ShippingOption{Carrier: "AusPost", Method: "Standard", Cost: Money{cost, "AUD"}, EstimatedDays: 5},
		ShippingOption{Carrier: "AusPost", Method: "Express", Cost: Money{cost + 500, "AUD"}, EstimatedDays: 2},
	)
	return options
}

func (c *ShippingCalculator) internationalOptions(cart Cart, dest Address) []ShippingOption {
	zone, ok := c.rates.CountryZones[dest.Country]
	if !ok {
		zone = 4
	}
	tiers := c.rates.ZoneRates[zone]
	cost := tieredCost(cart.WeightGrams, tiers)
	return []ShippingOption{
		{Carrier: "DHL", Method: "International Standard", Cost: Money{cost, "AUD"}, EstimatedDays: 10 + zone*3},
		{Carrier: "DHL", Method: "International Express", Cost: Money{cost + 2000, "AUD"}, EstimatedDays: 3 + zone},
	}
}

func tieredCost(weightGrams int, tiers []weightTier) int {
	for _, tier := range tiers {
		if weightGrams <= tier.MaxGrams {
			return tier.Cents
		}
	}
	if len(tiers) > 0 {
		return tiers[len(tiers)-1].Cents
	}
	return 0
}

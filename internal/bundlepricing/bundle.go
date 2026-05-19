// Package bundlepricing provides configurable product bundle pricing with
// support for compound discount stacking.
package bundlepricing

// Product is a catalogue item with a base price.
type Product struct {
	ID        string
	Name      string
	BasePrice float64
}

// BundleItem references a product and the quantity included in a bundle.
type BundleItem struct {
	ProductID string
	Quantity  int
}

// Bundle groups products together with an optional percentage discount.
type Bundle struct {
	ID         string
	Name       string
	Products   []BundleItem
	DiscountPct float64
}

// BundlePrice holds the pricing breakdown for a bundle calculation.
type BundlePrice struct {
	Subtotal float64
	Discount float64
	Total    float64
}

// Calculator computes the price of a bundle given a map of base prices.
type Calculator struct{}

// NewCalculator returns a Calculator.
func NewCalculator() Calculator { return Calculator{} }

// Calculate returns the BundlePrice for the given bundle and price map.
// Unknown product IDs contribute zero to the subtotal.
func (c Calculator) Calculate(b Bundle, prices map[string]float64) BundlePrice {
	var subtotal float64
	for _, item := range b.Products {
		subtotal += prices[item.ProductID] * float64(item.Quantity)
	}
	discount := subtotal * (b.DiscountPct / 100)
	return BundlePrice{
		Subtotal: subtotal,
		Discount: discount,
		Total:    subtotal - discount,
	}
}

// DiscountStack applies multiple percentage discounts sequentially (compound).
type DiscountStack struct {
	discounts []float64
}

// NewDiscountStack returns an empty DiscountStack.
func NewDiscountStack() *DiscountStack {
	return &DiscountStack{}
}

// Add appends a percentage discount to the stack.
func (d *DiscountStack) Add(pct float64) {
	d.discounts = append(d.discounts, pct)
}

// Apply returns the price after all stacked discounts are applied in order.
// Each discount reduces the running price: price *= (1 - pct/100).
func (d *DiscountStack) Apply(price float64) float64 {
	for _, pct := range d.discounts {
		price *= 1 - pct/100
	}
	return price
}

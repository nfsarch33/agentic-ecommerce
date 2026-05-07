package order

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

var ErrMissingSessionID = errors.New("missing cart session id")

type CartItemInput struct {
	ProductID uuid.UUID
	SKU       string
	Title     string
	Quantity  int
	UnitPrice catalog.Money
}

type CartItem struct {
	productID uuid.UUID
	sku       string
	title     string
	quantity  int
	unitPrice catalog.Money
	lineTotal catalog.Money
}

type Cart struct {
	sessionID string
	items     []CartItem
	totals    Totals
	updatedAt time.Time
}

func NewCart(sessionID string) (Cart, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Cart{}, ErrMissingSessionID
	}
	return Cart{
		sessionID: sessionID,
		totals:    Totals{Subtotal: catalog.ZeroAUD(), Shipping: catalog.ZeroAUD(), Total: catalog.ZeroAUD()},
		updatedAt: time.Now().UTC(),
	}, nil
}

type CartRecord struct {
	SessionID string
	Items     []CartItem
	Totals    Totals
	UpdatedAt time.Time
}

func ReconstructCart(rec CartRecord) Cart {
	items := append([]CartItem(nil), rec.Items...)
	return Cart{sessionID: rec.SessionID, items: items, totals: rec.Totals, updatedAt: rec.UpdatedAt}
}

func (c *Cart) ReplaceItems(inputs []CartItemInput) error {
	if len(inputs) == 0 {
		c.items = nil
		c.totals = Totals{Subtotal: catalog.ZeroAUD(), Shipping: catalog.ZeroAUD(), Total: catalog.ZeroAUD()}
		c.updatedAt = time.Now().UTC()
		return nil
	}

	items, totals, err := buildCartItems(inputs)
	if err != nil {
		return err
	}
	c.items = items
	c.totals = totals
	c.updatedAt = time.Now().UTC()
	return nil
}

func (c Cart) SessionID() string            { return c.sessionID }
func (c Cart) Items() []CartItem            { return append([]CartItem(nil), c.items...) }
func (c Cart) Totals() Totals               { return c.totals }
func (c Cart) UpdatedAt() time.Time         { return c.updatedAt }
func (c Cart) IsEmpty() bool                { return len(c.items) == 0 }
func (i CartItem) ProductID() uuid.UUID     { return i.productID }
func (i CartItem) SKU() string              { return i.sku }
func (i CartItem) Title() string            { return i.title }
func (i CartItem) Quantity() int            { return i.quantity }
func (i CartItem) UnitPrice() catalog.Money { return i.unitPrice }
func (i CartItem) LineTotal() catalog.Money { return i.lineTotal }

func ReconstructCartItem(input CartItemInput, lineTotal catalog.Money) CartItem {
	return CartItem{
		productID: input.ProductID,
		sku:       strings.ToUpper(strings.TrimSpace(input.SKU)),
		title:     strings.TrimSpace(input.Title),
		quantity:  input.Quantity,
		unitPrice: input.UnitPrice,
		lineTotal: lineTotal,
	}
}

func buildCartItems(inputs []CartItemInput) ([]CartItem, Totals, error) {
	currency := ""
	subtotalAmount := 0
	items := make([]CartItem, 0, len(inputs))
	for _, input := range inputs {
		item, err := newCartItem(input)
		if err != nil {
			return nil, Totals{}, err
		}
		if currency == "" {
			currency = item.UnitPrice().Currency()
		}
		if item.UnitPrice().Currency() != currency {
			return nil, Totals{}, ErrCurrencyMismatch
		}
		subtotalAmount += item.LineTotal().Amount()
		items = append(items, item)
	}

	subtotal, _ := catalog.NewMoney(subtotalAmount, currency)
	shipping, _ := catalog.NewMoney(0, currency)
	total, _ := catalog.NewMoney(subtotalAmount, currency)
	return items, Totals{Subtotal: subtotal, Shipping: shipping, Total: total}, nil
}

func newCartItem(input CartItemInput) (CartItem, error) {
	if input.ProductID == uuid.Nil || strings.TrimSpace(input.SKU) == "" || strings.TrimSpace(input.Title) == "" {
		return CartItem{}, ErrMissingItemIdentity
	}
	if input.Quantity <= 0 {
		return CartItem{}, ErrInvalidQuantity
	}
	lineTotal, err := catalog.NewMoney(input.UnitPrice.Amount()*input.Quantity, input.UnitPrice.Currency())
	if err != nil {
		return CartItem{}, err
	}
	return ReconstructCartItem(input, lineTotal), nil
}

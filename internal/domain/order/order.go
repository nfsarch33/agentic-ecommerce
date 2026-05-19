package order

import (
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
)

var (
	ErrInvalidCustomerEmail = errors.New("invalid customer email")
	ErrOrderRequiresItems   = errors.New("order requires at least one item")
	ErrInvalidQuantity      = errors.New("quantity must be greater than zero")
	ErrMissingShippingField = errors.New("missing shipping address field")
	ErrCurrencyMismatch     = errors.New("order items must use one currency")
	ErrMissingItemIdentity  = errors.New("missing item identity")
)

type ShippingAddress struct {
	Name       string
	Line1      string
	Line2      string
	City       string
	Region     string
	PostalCode string
	Country    string
}

type Totals struct {
	Subtotal catalog.Money
	Shipping catalog.Money
	Total    catalog.Money
}

type OrderItemInput struct {
	ProductID uuid.UUID
	SKU       string
	Title     string
	Quantity  int
	UnitPrice catalog.Money
}

type OrderInput struct {
	CustomerEmail   string
	Items           []OrderItemInput
	ShippingAddress ShippingAddress
	Shipping        catalog.Money
}

type OrderItem struct {
	productID uuid.UUID
	sku       string
	title     string
	quantity  int
	unitPrice catalog.Money
	lineTotal catalog.Money
}

type Order struct {
	id              uuid.UUID
	customerEmail   string
	items           []OrderItem
	status          Status
	totals          Totals
	shippingAddress ShippingAddress
	createdAt       time.Time
	updatedAt       time.Time
}

func NewOrder(input OrderInput) (Order, error) {
	email, err := normaliseEmail(input.CustomerEmail)
	if err != nil {
		return Order{}, err
	}
	if err := validateShippingAddress(input.ShippingAddress); err != nil {
		return Order{}, err
	}
	items, totals, err := buildOrderItems(input.Items, input.Shipping)
	if err != nil {
		return Order{}, err
	}

	now := time.Now().UTC()
	return Order{
		id:              uuid.New(),
		customerEmail:   email,
		items:           items,
		status:          StatusPending,
		totals:          totals,
		shippingAddress: normaliseShippingAddress(input.ShippingAddress),
		createdAt:       now,
		updatedAt:       now,
	}, nil
}

type OrderRecord struct {
	ID              uuid.UUID
	CustomerEmail   string
	Items           []OrderItem
	Status          Status
	Totals          Totals
	ShippingAddress ShippingAddress
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func ReconstructOrder(rec OrderRecord) Order {
	items := append([]OrderItem(nil), rec.Items...)
	return Order{
		id:              rec.ID,
		customerEmail:   rec.CustomerEmail,
		items:           items,
		status:          rec.Status,
		totals:          rec.Totals,
		shippingAddress: rec.ShippingAddress,
		createdAt:       rec.CreatedAt,
		updatedAt:       rec.UpdatedAt,
	}
}

func (o *Order) AdvanceStatus(next Status) error {
	if !canTransition(o.status, next) {
		return ErrInvalidStatusTransition
	}
	o.status = next
	o.updatedAt = time.Now().UTC()
	return nil
}

func (o Order) ID() uuid.UUID                    { return o.id }
func (o Order) CustomerEmail() string            { return o.customerEmail }
func (o Order) Items() []OrderItem               { return append([]OrderItem(nil), o.items...) }
func (o Order) Status() Status                   { return o.status }
func (o Order) Totals() Totals                   { return o.totals }
func (o Order) ShippingAddress() ShippingAddress { return o.shippingAddress }
func (o Order) CreatedAt() time.Time             { return o.createdAt }
func (o Order) UpdatedAt() time.Time             { return o.updatedAt }

func (i OrderItem) ProductID() uuid.UUID     { return i.productID }
func (i OrderItem) SKU() string              { return i.sku }
func (i OrderItem) Title() string            { return i.title }
func (i OrderItem) Quantity() int            { return i.quantity }
func (i OrderItem) UnitPrice() catalog.Money { return i.unitPrice }
func (i OrderItem) LineTotal() catalog.Money { return i.lineTotal }

func ReconstructOrderItem(input OrderItemInput, lineTotal catalog.Money) OrderItem {
	return OrderItem{
		productID: input.ProductID,
		sku:       strings.ToUpper(strings.TrimSpace(input.SKU)),
		title:     strings.TrimSpace(input.Title),
		quantity:  input.Quantity,
		unitPrice: input.UnitPrice,
		lineTotal: lineTotal,
	}
}

func normaliseEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return "", ErrInvalidCustomerEmail
	}
	return email, nil
}

func validateShippingAddress(address ShippingAddress) error {
	normalised := normaliseShippingAddress(address)
	if normalised.Name == "" || normalised.Line1 == "" || normalised.City == "" || normalised.PostalCode == "" || normalised.Country == "" {
		return ErrMissingShippingField
	}
	return nil
}

func normaliseShippingAddress(address ShippingAddress) ShippingAddress {
	return ShippingAddress{
		Name:       strings.TrimSpace(address.Name),
		Line1:      strings.TrimSpace(address.Line1),
		Line2:      strings.TrimSpace(address.Line2),
		City:       strings.TrimSpace(address.City),
		Region:     strings.TrimSpace(address.Region),
		PostalCode: strings.TrimSpace(address.PostalCode),
		Country:    strings.ToUpper(strings.TrimSpace(address.Country)),
	}
}

func buildOrderItems(inputs []OrderItemInput, shipping catalog.Money) ([]OrderItem, Totals, error) {
	if len(inputs) == 0 {
		return nil, Totals{}, ErrOrderRequiresItems
	}

	currency := ""
	subtotalAmount := 0
	items := make([]OrderItem, 0, len(inputs))
	for _, input := range inputs {
		item, err := newOrderItem(input)
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

	if shipping.IsZero() {
		var err error
		shipping, err = catalog.NewMoney(0, currency)
		if err != nil {
			return nil, Totals{}, err
		}
	}
	if shipping.Currency() != currency {
		return nil, Totals{}, ErrCurrencyMismatch
	}
	subtotal, _ := catalog.NewMoney(subtotalAmount, currency)
	total, _ := catalog.NewMoney(subtotalAmount+shipping.Amount(), currency)

	return items, Totals{Subtotal: subtotal, Shipping: shipping, Total: total}, nil
}

func newOrderItem(input OrderItemInput) (OrderItem, error) {
	if input.ProductID == uuid.Nil || strings.TrimSpace(input.SKU) == "" || strings.TrimSpace(input.Title) == "" {
		return OrderItem{}, ErrMissingItemIdentity
	}
	if input.Quantity <= 0 {
		return OrderItem{}, ErrInvalidQuantity
	}
	lineTotal, err := catalog.NewMoney(input.UnitPrice.Amount()*input.Quantity, input.UnitPrice.Currency())
	if err != nil {
		return OrderItem{}, err
	}
	return ReconstructOrderItem(input, lineTotal), nil
}

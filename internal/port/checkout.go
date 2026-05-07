package port

import (
	"context"

	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	orderdomain "github.com/nfsarch33/agentic-ecommerce/internal/domain/order"
)

type PaymentGateway interface {
	Authorize(ctx context.Context, order orderdomain.Order) (PaymentAuthorization, error)
}

type PaymentAuthorization struct {
	ID     string
	Amount catalog.Money
}

type ShippingProvider interface {
	CreateShipment(ctx context.Context, order orderdomain.Order) (Shipment, error)
}

type Shipment struct {
	ID      string
	Carrier string
	Status  string
}

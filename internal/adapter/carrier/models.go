package carrier

import "time"

// CarrierName names a carrier. Stable string used in metrics labels +
// EvoMap KPI samples. Bounded cardinality.
const (
	CarrierAusPost = "auspost"
	CarrierDHL     = "dhl"
)

// QuoteRequest is the shared input shape submitted to a carrier
// client's Quote method.
type QuoteRequest struct {
	TenantID      string
	OriginCountry string
	OriginPost    string
	DestCountry   string
	DestPost      string
	WeightGrams   int
}

// Quote is the carrier's pricing + ETA response.
type Quote struct {
	Carrier      string
	CostAUDCents int
	ETADays      int
}

// LabelRequest is the shared input shape submitted to a carrier
// client's CreateLabel method.
type LabelRequest struct {
	TenantID    string
	OrderID     string
	BuyerEmail  string
	OriginPost  string
	DestPost    string
	DestCountry string
	WeightGrams int
}

// Label is the carrier's confirmation: tracking number + PDF URL +
// quoted ETA + locked-in cost.
type Label struct {
	Carrier        string
	TrackingNumber string
	LabelPDFURL    string
	CostAUDCents   int
	ETADays        int
	GeneratedAt    time.Time
}

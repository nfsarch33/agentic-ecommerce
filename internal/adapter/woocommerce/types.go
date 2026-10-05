package woocommerce

type Product struct {
	ID            int                `json:"id,omitempty"`
	Name          string             `json:"name"`
	Type          string             `json:"type,omitempty"`
	Status        string             `json:"status,omitempty"`
	Regular       string             `json:"regular_price,omitempty"`
	Price         string             `json:"price,omitempty"`
	Description   string             `json:"description,omitempty"`
	ShortDesc     string             `json:"short_description,omitempty"`
	SKU           string             `json:"sku,omitempty"`
	ManageStock   *bool              `json:"manage_stock,omitempty"`
	StockQuantity *int               `json:"stock_quantity,omitempty"`
	Categories    []IDRef            `json:"categories,omitempty"`
	Tags          []IDRef            `json:"tags,omitempty"`
	Attributes    []ProductAttribute `json:"attributes,omitempty"`
	Images        []Image            `json:"images,omitempty"`
}

// ProductAttribute is one declared attribute with its offered terms; the
// grounding check reads exactly these values.
type ProductAttribute struct {
	Name    string   `json:"name"`
	Options []string `json:"options,omitempty"`
}

type IDRef struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
}

type Image struct {
	Src string `json:"src"`
	Alt string `json:"alt,omitempty"`
}

type Order struct {
	ID            int             `json:"id"`
	Status        string          `json:"status"`
	Total         string          `json:"total"`
	Currency      string          `json:"currency"`
	DateCreated   string          `json:"date_created,omitempty"`
	PaymentMethod string          `json:"payment_method,omitempty"`
	Billing       OrderBilling    `json:"billing"`
	LineItems     []OrderLineItem `json:"line_items"`
	// Refunds carries the order's refund set (each entry's Total is the
	// refunded amount, negative in WooCommerce's JSON). Additive: the field
	// rides the orders payload the API already returns; the ops digest is
	// its first consumer.
	Refunds []OrderRefund `json:"refunds,omitempty"`
}

type OrderRefund struct {
	RefundID int    `json:"refund_id"`
	Reason   string `json:"reason,omitempty"`
	Total    string `json:"total"`
}

type OrderBilling struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

type OrderLineItem struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ProductID int    `json:"product_id"`
	Quantity  int    `json:"quantity"`
	Total     string `json:"total"`
}

type ListOptions struct {
	PerPage int
	Page    int
	Status  string
	After   string
	Before  string
	SKU     string
}

type BatchResult struct {
	Create []Product `json:"create,omitempty"`
}

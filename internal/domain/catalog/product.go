package catalog

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const MaxDescriptionRunes = 5000

var (
	ErrMissingSKU          = errors.New("missing sku")
	ErrMissingTitle        = errors.New("missing title")
	ErrDescriptionTooLarge = errors.New("description too large")
	ErrMissingID           = errors.New("missing product id")
)

var slugRe = regexp.MustCompile(`[^a-z0-9-]+`)

type ProductInput struct {
	SKU         string
	Title       string
	Slug        string
	Description string
	Price       Money
	Stock       int
	Status      ProductStatus
	Images      []Image
	Categories  []Category
}

type Product struct {
	id          uuid.UUID
	sku         string
	title       string
	slug        string
	description string
	price       Money
	stock       int
	status      ProductStatus
	images      []Image
	categories  []Category
	createdAt   time.Time
	updatedAt   time.Time
}

func NewProduct(input ProductInput) (Product, error) {
	sku := strings.ToUpper(strings.TrimSpace(input.SKU))
	if sku == "" {
		return Product{}, ErrMissingSKU
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		return Product{}, ErrMissingTitle
	}

	description := strings.TrimSpace(input.Description)
	if utf8.RuneCountInString(description) > MaxDescriptionRunes {
		return Product{}, ErrDescriptionTooLarge
	}

	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		slug = generateSlug(title)
	}

	status := input.Status
	if status == "" {
		status = StatusDraft
	}

	now := time.Now().UTC()

	return Product{
		id:          uuid.New(),
		sku:         sku,
		title:       title,
		slug:        slug,
		description: description,
		price:       input.Price,
		stock:       input.Stock,
		status:      status,
		images:      input.Images,
		categories:  input.Categories,
		createdAt:   now,
		updatedAt:   now,
	}, nil
}

// ProductRecord is used exclusively to reconstruct a Product from persistence
// without re-running validation. Only repository adapters should use this.
type ProductRecord struct {
	ID          uuid.UUID
	SKU         string
	Title       string
	Slug        string
	Description string
	Price       Money
	Stock       int
	Status      ProductStatus
	Images      []Image
	Categories  []Category
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func ReconstructProduct(rec ProductRecord) Product {
	return Product{
		id:          rec.ID,
		sku:         rec.SKU,
		title:       rec.Title,
		slug:        rec.Slug,
		description: rec.Description,
		price:       rec.Price,
		stock:       rec.Stock,
		status:      rec.Status,
		images:      rec.Images,
		categories:  rec.Categories,
		createdAt:   rec.CreatedAt,
		updatedAt:   rec.UpdatedAt,
	}
}

func (p Product) ID() uuid.UUID          { return p.id }
func (p Product) SKU() string            { return p.sku }
func (p Product) Title() string          { return p.title }
func (p Product) Slug() string           { return p.slug }
func (p Product) Description() string    { return p.description }
func (p Product) Price() Money           { return p.price }
func (p Product) Stock() int             { return p.stock }
func (p Product) Status() ProductStatus  { return p.status }
func (p Product) Images() []Image        { return p.images }
func (p Product) Categories() []Category { return p.categories }
func (p Product) CreatedAt() time.Time   { return p.createdAt }
func (p Product) UpdatedAt() time.Time   { return p.updatedAt }

func generateSlug(title string) string {
	s := strings.ToLower(title)
	s = strings.ReplaceAll(s, " ", "-")
	s = slugRe.ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	return s
}

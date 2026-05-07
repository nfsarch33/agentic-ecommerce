package catalog

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const MaxDescriptionRunes = 5000

var (
	ErrMissingSKU          = errors.New("missing sku")
	ErrMissingTitle        = errors.New("missing title")
	ErrDescriptionTooLarge = errors.New("description too large")
)

type ProductInput struct {
	SKU         string
	Title       string
	Description string
}

type Product struct {
	sku         string
	title       string
	description string
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

	return Product{
		sku:         sku,
		title:       title,
		description: description,
	}, nil
}

func (p Product) SKU() string {
	return p.sku
}

func (p Product) Title() string {
	return p.title
}

func (p Product) Description() string {
	return p.description
}

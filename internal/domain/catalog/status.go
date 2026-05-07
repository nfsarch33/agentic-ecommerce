package catalog

import "errors"

var ErrInvalidStatus = errors.New("invalid product status")

type ProductStatus string

const (
	StatusDraft    ProductStatus = "draft"
	StatusActive   ProductStatus = "active"
	StatusArchived ProductStatus = "archived"
)

func ParseProductStatus(s string) (ProductStatus, error) {
	switch ProductStatus(s) {
	case StatusDraft, StatusActive, StatusArchived:
		return ProductStatus(s), nil
	default:
		return "", ErrInvalidStatus
	}
}

func (s ProductStatus) String() string { return string(s) }

func (s ProductStatus) IsPublishable() bool { return s == StatusActive }

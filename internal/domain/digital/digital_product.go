package digital

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// DigitalProduct is the catalogue entry for a downloadable artefact
// (PDF, ZIP, video, etc.). It is tenant-scoped and carries the storage
// pointer (FilePath) plus integrity metadata (FileSize, Checksum) so
// download tokens can be issued against an immutable snapshot.
type DigitalProduct struct {
	id          uuid.UUID
	tenantID    string
	sku         string
	name        string
	description string
	filePath    string
	fileSize    int64
	contentType string
	checksum    string
	version     string
	createdAt   time.Time
	updatedAt   time.Time
}

// DigitalProductInput is the constructor payload for a DigitalProduct.
type DigitalProductInput struct {
	TenantID    string
	SKU         string
	Name        string
	Description string
	FilePath    string
	FileSize    int64
	ContentType string
	Checksum    string
	Version     string
	Now         time.Time
}

// DigitalProductRecord is the repository hydration shape.
type DigitalProductRecord struct {
	ID          uuid.UUID
	TenantID    string
	SKU         string
	Name        string
	Description string
	FilePath    string
	FileSize    int64
	ContentType string
	Checksum    string
	Version     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewDigitalProduct constructs a DigitalProduct with field validation.
func NewDigitalProduct(input DigitalProductInput) (DigitalProduct, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		return DigitalProduct{}, ErrTenantRequired
	}
	sku := strings.TrimSpace(input.SKU)
	if sku == "" {
		return DigitalProduct{}, ErrSKURequired
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return DigitalProduct{}, ErrNameRequired
	}
	filePath := strings.TrimSpace(input.FilePath)
	if filePath == "" {
		return DigitalProduct{}, ErrFilePathRequired
	}
	if input.FileSize <= 0 {
		return DigitalProduct{}, ErrInvalidFileSize
	}
	version := strings.TrimSpace(input.Version)
	if version == "" {
		return DigitalProduct{}, ErrInvalidVersion
	}

	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	return DigitalProduct{
		id:          uuid.New(),
		tenantID:    tenantID,
		sku:         sku,
		name:        name,
		description: strings.TrimSpace(input.Description),
		filePath:    filePath,
		fileSize:    input.FileSize,
		contentType: strings.TrimSpace(input.ContentType),
		checksum:    strings.TrimSpace(input.Checksum),
		version:     version,
		createdAt:   now,
		updatedAt:   now,
	}, nil
}

// ReconstructDigitalProduct hydrates a DigitalProduct from a repository
// record without re-validating; adapters call this after reading rows.
func ReconstructDigitalProduct(rec DigitalProductRecord) DigitalProduct {
	return DigitalProduct{
		id:          rec.ID,
		tenantID:    rec.TenantID,
		sku:         rec.SKU,
		name:        rec.Name,
		description: rec.Description,
		filePath:    rec.FilePath,
		fileSize:    rec.FileSize,
		contentType: rec.ContentType,
		checksum:    rec.Checksum,
		version:     rec.Version,
		createdAt:   rec.CreatedAt,
		updatedAt:   rec.UpdatedAt,
	}
}

// Update applies partial mutations to a DigitalProduct. Empty input
// fields leave existing values untouched. Now must be supplied so
// callers stay deterministic.
func (p *DigitalProduct) Update(input DigitalProductInput, now time.Time) error {
	if name := strings.TrimSpace(input.Name); name != "" {
		p.name = name
	}
	if description := strings.TrimSpace(input.Description); description != "" {
		p.description = description
	}
	if filePath := strings.TrimSpace(input.FilePath); filePath != "" {
		p.filePath = filePath
	}
	if input.FileSize > 0 {
		p.fileSize = input.FileSize
	}
	if contentType := strings.TrimSpace(input.ContentType); contentType != "" {
		p.contentType = contentType
	}
	if checksum := strings.TrimSpace(input.Checksum); checksum != "" {
		p.checksum = checksum
	}
	if version := strings.TrimSpace(input.Version); version != "" {
		p.version = version
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	p.updatedAt = now.UTC()
	return nil
}

// Accessors. Internal fields are unexported so the only mutation paths
// are the constructor and the explicit Update method; this prevents
// callers from bypassing validation.

func (p DigitalProduct) ID() uuid.UUID       { return p.id }
func (p DigitalProduct) TenantID() string    { return p.tenantID }
func (p DigitalProduct) SKU() string         { return p.sku }
func (p DigitalProduct) Name() string        { return p.name }
func (p DigitalProduct) Description() string { return p.description }
func (p DigitalProduct) FilePath() string    { return p.filePath }
func (p DigitalProduct) FileSize() int64     { return p.fileSize }
func (p DigitalProduct) ContentType() string { return p.contentType }
func (p DigitalProduct) Checksum() string    { return p.checksum }
func (p DigitalProduct) Version() string     { return p.version }
func (p DigitalProduct) CreatedAt() time.Time {
	return p.createdAt
}
func (p DigitalProduct) UpdatedAt() time.Time { return p.updatedAt }

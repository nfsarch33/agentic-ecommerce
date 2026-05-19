package returnlabel

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"
)

// Carrier constants.
const (
	CarrierAusPost = "auspost"
	CarrierFedEx   = "fedex"
	CarrierUPS     = "ups"
	CarrierDHL     = "dhl"
)

// Sentinel errors.
var (
	ErrLabelNotFound = errors.New("returnlabel: label not found")
)

// Address is a postal address.
type Address struct {
	Name    string
	Line1   string
	City    string
	State   string
	Zip     string
	Country string
}

// ReturnRequest contains all information needed to generate a return label.
type ReturnRequest struct {
	OrderID      string
	CustomerName string
	From         Address
	To           Address
	Carrier      string
	Weight       float64
}

// Label represents a generated shipping label.
type Label struct {
	ID         string
	Carrier    string
	TrackingNo string
	QRData     string
	CreatedAt  time.Time
	Raw        []byte
}

// Generator is the interface implemented by label generation backends.
type Generator interface {
	Generate(ctx context.Context, req ReturnRequest) (Label, error)
}

// QRData returns a URL-encoded string suitable for QR encoding from a tracking number.
func QRData(tracking string) string {
	return "https://track.helixon.io/?tracking=" + url.QueryEscape(tracking)
}

// StubGenerator is a deterministic test double for Generator.
// It produces labels whose tracking numbers are derived from carrier + orderID.
type StubGenerator struct{}

// Generate returns a deterministic Label based on the carrier and order ID.
func (g *StubGenerator) Generate(_ context.Context, req ReturnRequest) (Label, error) {
	if req.OrderID == "" {
		return Label{}, fmt.Errorf("returnlabel: order ID must not be empty")
	}
	tracking := req.Carrier + "-" + req.OrderID
	id := "lbl-" + tracking
	qr := QRData(tracking)
	return Label{
		ID:         id,
		Carrier:    req.Carrier,
		TrackingNo: tracking,
		QRData:     qr,
		CreatedAt:  time.Now(),
		Raw:        []byte("stub-label:" + id),
	}, nil
}

// LabelStore is a thread-safe in-memory store for Label records.
type LabelStore struct {
	mu     sync.RWMutex
	byID   map[string]*Label
	byOrder map[string][]*Label
}

// Save persists a label in the store.
func (ls *LabelStore) Save(label Label) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.byID == nil {
		ls.byID = make(map[string]*Label)
		ls.byOrder = make(map[string][]*Label)
	}
	cp := label
	ls.byID[label.ID] = &cp
	ls.byOrder[label.Carrier] = append(ls.byOrder[label.Carrier], &cp)
}

// Get retrieves a label by ID. Returns ErrLabelNotFound if absent.
func (ls *LabelStore) Get(id string) (*Label, error) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	l, ok := ls.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: id=%s", ErrLabelNotFound, id)
	}
	cp := *l
	return &cp, nil
}

// ByOrder returns all labels associated with the given orderID.
// The store is keyed by carrier internally; callers supply the orderID prefix
// to match against TrackingNo.
func (ls *LabelStore) ByOrder(orderID string) []Label {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	var out []Label
	for _, l := range ls.byID {
		// TrackingNo format from StubGenerator: carrier-orderID
		// We match any label whose tracking number ends with the order ID segment.
		if containsOrderID(l.TrackingNo, orderID) {
			out = append(out, *l)
		}
	}
	return out
}

// containsOrderID checks whether the tracking number contains the given order ID
// (after the first "-").
func containsOrderID(tracking, orderID string) bool {
	for i := 0; i < len(tracking); i++ {
		if tracking[i] == '-' && i+1 < len(tracking) {
			if tracking[i+1:] == orderID {
				return true
			}
		}
	}
	return false
}

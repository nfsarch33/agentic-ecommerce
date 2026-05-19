// Package wishlist provides wishlist management with sharing, price-drop
// detection, and analytics.
package wishlist

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	ErrNotFound     = errors.New("wishlist not found")
	ErrItemNotFound = errors.New("item not found")
)

// Item represents a single product entry in a wishlist.
type Item struct {
	ProductID    string
	AddedAt      time.Time
	PriceAtAdd   float64
	NotifyOnDrop bool
}

// Wishlist holds a user's saved products.
type Wishlist struct {
	ID         string
	OwnerID    string
	Items      map[string]Item
	ShareToken string
	CreatedAt  time.Time
}

// PriceDrop describes a price decrease for a wishlist item.
type PriceDrop struct {
	ProductID string
	OldPrice  float64
	NewPrice  float64
	DropPct   float64
}

// ProductCount aggregates the number of wishlists containing a product.
type ProductCount struct {
	ProductID string
	Count     int
}

// Service is a thread-safe wishlist repository.
type Service struct {
	mu        sync.RWMutex
	wishlists map[string]*Wishlist
	counter   int
}

// NewService creates an empty Service.
func NewService() *Service {
	return &Service{wishlists: make(map[string]*Wishlist)}
}

// Create initialises a new wishlist for ownerID.
func (svc *Service) Create(ownerID string) *Wishlist {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	svc.counter++
	wl := &Wishlist{
		ID:        generateID(),
		OwnerID:   ownerID,
		Items:     make(map[string]Item),
		CreatedAt: time.Now(),
	}
	svc.wishlists[wl.ID] = wl
	return wl
}

// Get returns a wishlist by ID.
func (svc *Service) Get(id string) (*Wishlist, error) {
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	wl, ok := svc.wishlists[id]
	if !ok {
		return nil, ErrNotFound
	}
	return wl, nil
}

// AddItem adds or updates a product in the wishlist.
func (svc *Service) AddItem(id, productID string, price float64, notify bool) error {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	wl, ok := svc.wishlists[id]
	if !ok {
		return ErrNotFound
	}
	wl.Items[productID] = Item{
		ProductID:    productID,
		AddedAt:      time.Now(),
		PriceAtAdd:   price,
		NotifyOnDrop: notify,
	}
	return nil
}

// RemoveItem deletes a product from the wishlist.
func (svc *Service) RemoveItem(id, productID string) error {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	wl, ok := svc.wishlists[id]
	if !ok {
		return ErrNotFound
	}
	if _, exists := wl.Items[productID]; !exists {
		return ErrItemNotFound
	}
	delete(wl.Items, productID)
	return nil
}

// GenerateShareToken creates a new random share token for the wishlist.
func (svc *Service) GenerateShareToken(id string) string {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	wl, ok := svc.wishlists[id]
	if !ok {
		return ""
	}
	token := randomHex(16)
	wl.ShareToken = token
	return token
}

// PriceDropDetector checks wishlist items against current prices.
type PriceDropDetector struct{}

// Check returns PriceDrop entries for items whose current price is lower than
// the recorded price-at-add and have NotifyOnDrop set.
func (d PriceDropDetector) Check(wl *Wishlist, currentPrices map[string]float64) []PriceDrop {
	var drops []PriceDrop
	for _, item := range wl.Items {
		if !item.NotifyOnDrop {
			continue
		}
		cur, ok := currentPrices[item.ProductID]
		if !ok || cur >= item.PriceAtAdd {
			continue
		}
		dropPct := (item.PriceAtAdd - cur) / item.PriceAtAdd * 100
		drops = append(drops, PriceDrop{
			ProductID: item.ProductID,
			OldPrice:  item.PriceAtAdd,
			NewPrice:  cur,
			DropPct:   dropPct,
		})
	}
	return drops
}

// MostWishlisted returns products sorted by how many wishlists contain them,
// descending.
func MostWishlisted(svc *Service) []ProductCount {
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	counts := make(map[string]int)
	for _, wl := range svc.wishlists {
		for pid := range wl.Items {
			counts[pid]++
		}
	}
	result := make([]ProductCount, 0, len(counts))
	for pid, c := range counts {
		result = append(result, ProductCount{ProductID: pid, Count: c})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})
	return result
}

func generateID() string {
	return randomHex(8)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// fallback: timestamp-based (should never happen)
		return hex.EncodeToString([]byte(time.Now().String()))[:n*2]
	}
	return hex.EncodeToString(b)
}

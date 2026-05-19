package orderrouting

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

// Sentinel errors.
var (
	ErrNoAvailableCenter = errors.New("orderrouting: no available fulfillment center")
	ErrCenterNotFound    = errors.New("orderrouting: fulfillment center not found")
	ErrInsufficientCap   = errors.New("orderrouting: insufficient capacity")
)

// Location is a geographic coordinate.
type Location struct {
	Lat float64
	Lon float64
}

// FulfillmentCenter represents a warehouse/fulfillment center.
type FulfillmentCenter struct {
	ID       string
	Name     string
	Location Location
	Capacity int
	Active   bool
}

// DistanceKM calculates the great-circle distance between two locations using
// the Haversine formula. Returns the distance in kilometres.
func DistanceKM(a, b Location) float64 {
	const earthRadiusKM = 6371.0

	lat1 := a.Lat * math.Pi / 180
	lat2 := b.Lat * math.Pi / 180
	dLat := (b.Lat - a.Lat) * math.Pi / 180
	dLon := (b.Lon - a.Lon) * math.Pi / 180

	sinLat := math.Sin(dLat / 2)
	sinLon := math.Sin(dLon / 2)

	h := sinLat*sinLat + math.Cos(lat1)*math.Cos(lat2)*sinLon*sinLon
	c := 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
	return earthRadiusKM * c
}

// Router is a thread-safe registry of FulfillmentCenter entries.
type Router struct {
	mu      sync.RWMutex
	centers map[string]FulfillmentCenter
}

// AddCenter inserts or replaces a FulfillmentCenter.
func (r *Router) AddCenter(fc FulfillmentCenter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.centers == nil {
		r.centers = make(map[string]FulfillmentCenter)
	}
	r.centers[fc.ID] = fc
}

// RemoveCenter deletes the center with the given ID (no-op if absent).
func (r *Router) RemoveCenter(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.centers, id)
}

// ActiveCenters returns a slice of all centers where Active == true.
func (r *Router) ActiveCenters() []FulfillmentCenter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []FulfillmentCenter
	for _, fc := range r.centers {
		if fc.Active {
			out = append(out, fc)
		}
	}
	return out
}

// RouteOrder selects the nearest active center with Capacity > 0.
// Returns ErrNoAvailableCenter if no qualifying center exists.
func RouteOrder(centers []FulfillmentCenter, destination Location) (*FulfillmentCenter, error) {
	var best *FulfillmentCenter
	bestDist := math.MaxFloat64

	for i := range centers {
		fc := centers[i]
		if !fc.Active || fc.Capacity <= 0 {
			continue
		}
		d := DistanceKM(fc.Location, destination)
		if d < bestDist {
			bestDist = d
			cp := fc
			best = &cp
		}
	}
	if best == nil {
		return nil, ErrNoAvailableCenter
	}
	return best, nil
}

// CapacityManager tracks remaining capacity per fulfillment center.
type CapacityManager struct {
	mu        sync.RWMutex
	remaining map[string]int
}

// SetCapacity initialises the capacity for a center (call before Reserve/Release).
func (cm *CapacityManager) SetCapacity(centerID string, capacity int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.remaining == nil {
		cm.remaining = make(map[string]int)
	}
	cm.remaining[centerID] = capacity
}

// Reserve reduces the remaining capacity of a center by qty.
// Returns ErrCenterNotFound if the center has not been initialised,
// or ErrInsufficientCap if remaining < qty.
func (cm *CapacityManager) Reserve(centerID string, qty int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.remaining == nil {
		return fmt.Errorf("%w: center=%s", ErrCenterNotFound, centerID)
	}
	rem, ok := cm.remaining[centerID]
	if !ok {
		return fmt.Errorf("%w: center=%s", ErrCenterNotFound, centerID)
	}
	if rem < qty {
		return fmt.Errorf("%w: remaining=%d requested=%d", ErrInsufficientCap, rem, qty)
	}
	cm.remaining[centerID] = rem - qty
	return nil
}

// Release increases the remaining capacity of a center by qty (no-op if not found).
func (cm *CapacityManager) Release(centerID string, qty int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.remaining == nil {
		return
	}
	if _, ok := cm.remaining[centerID]; ok {
		cm.remaining[centerID] += qty
	}
}

// Remaining returns the current remaining capacity for a center (0 if unknown).
func (cm *CapacityManager) Remaining(centerID string) int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.remaining[centerID]
}

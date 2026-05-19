package multiregion

import (
	"errors"
	"sync"
)

// Region describes a geographic deployment region.
type Region struct {
	Code                string
	Name                string
	Primary             bool
	DataResidencyZones  []string
}

// RegionStore is a thread-safe registry of regions.
type RegionStore struct {
	mu      sync.RWMutex
	regions map[string]Region
}

// NewRegionStore returns an initialised RegionStore.
func NewRegionStore() *RegionStore {
	return &RegionStore{regions: make(map[string]Region)}
}

// Add registers a region.
func (s *RegionStore) Add(r Region) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.regions[r.Code] = r
}

// Get retrieves a region by code.
func (s *RegionStore) Get(code string) (*Region, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.regions[code]
	if !ok {
		return nil, errors.New("multiregion: region not found: " + code)
	}
	cp := r
	return &cp, nil
}

// PrimaryRegion returns the primary region, or an error if none is configured.
func (s *RegionStore) PrimaryRegion() (*Region, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.regions {
		if r.Primary {
			cp := r
			return &cp, nil
		}
	}
	return nil, errors.New("multiregion: no primary region configured")
}

// ReadReplicas returns all non-primary regions.
func (s *RegionStore) ReadReplicas() []Region {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Region
	for _, r := range s.regions {
		if !r.Primary {
			out = append(out, r)
		}
	}
	return out
}

// ResidencyRule declares that a data type must remain in a specific zone.
type ResidencyRule struct {
	DataType     string
	RequiredZone string
}

// ResidencyEnforcer validates that a data type can be stored in the target region.
type ResidencyEnforcer struct{}

// Check returns an error if any residency rule prevents storing dataType in targetRegion.
func (ResidencyEnforcer) Check(dataType, targetRegion string, rules []ResidencyRule, store *RegionStore) error {
	region, err := store.Get(targetRegion)
	if err != nil {
		return err
	}

	for _, rule := range rules {
		if rule.DataType != dataType {
			continue
		}
		// The target region must include the required zone.
		found := false
		for _, zone := range region.DataResidencyZones {
			if zone == rule.RequiredZone {
				found = true
				break
			}
		}
		if !found {
			return errors.New("multiregion: data type " + dataType +
				" requires zone " + rule.RequiredZone +
				" which is not available in region " + targetRegion)
		}
	}
	return nil
}

// FailoverManager tracks and coordinates region failover.
type FailoverManager struct {
	mu     sync.RWMutex
	active string
}

// NewFailoverManager returns a FailoverManager with the given active region.
func NewFailoverManager(activeRegion string) *FailoverManager {
	return &FailoverManager{active: activeRegion}
}

// SetActive sets the active region directly.
func (f *FailoverManager) SetActive(regionCode string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active = regionCode
}

// Active returns the currently active region code.
func (f *FailoverManager) Active() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.active
}

// Failover switches the active region to the target and returns the previous region.
// Returns an error if the target is the same as the current active region.
func (f *FailoverManager) Failover(to string) (from string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.active == to {
		return f.active, errors.New("multiregion: already active in region " + to)
	}
	from = f.active
	f.active = to
	return from, nil
}

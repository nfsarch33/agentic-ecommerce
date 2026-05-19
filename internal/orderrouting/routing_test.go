package orderrouting

import (
	"errors"
	"math"
	"testing"
)

func TestDistanceKM_Known(t *testing.T) {
	t.Parallel()
	// Sydney (-33.8688, 151.2093) to Melbourne (-37.8136, 144.9631) ≈ 714 km
	sydney := Location{Lat: -33.8688, Lon: 151.2093}
	melbourne := Location{Lat: -37.8136, Lon: 144.9631}
	d := DistanceKM(sydney, melbourne)
	if math.Abs(d-714) > 10 {
		t.Errorf("expected ~714 km, got %.2f km", d)
	}
}

func TestDistanceKM_ZeroDistance(t *testing.T) {
	t.Parallel()
	p := Location{Lat: 0, Lon: 0}
	if d := DistanceKM(p, p); d != 0 {
		t.Errorf("expected 0 distance for same point, got %f", d)
	}
}

func TestRouter_AddAndActive(t *testing.T) {
	t.Parallel()
	r := &Router{}
	r.AddCenter(FulfillmentCenter{ID: "fc1", Name: "Alpha", Active: true, Capacity: 100})
	r.AddCenter(FulfillmentCenter{ID: "fc2", Name: "Beta", Active: false, Capacity: 50})
	r.AddCenter(FulfillmentCenter{ID: "fc3", Name: "Gamma", Active: true, Capacity: 200})

	active := r.ActiveCenters()
	if len(active) != 2 {
		t.Errorf("expected 2 active centers, got %d", len(active))
	}
}

func TestRouter_RemoveCenter(t *testing.T) {
	t.Parallel()
	r := &Router{}
	r.AddCenter(FulfillmentCenter{ID: "fc1", Active: true, Capacity: 10})
	r.AddCenter(FulfillmentCenter{ID: "fc2", Active: true, Capacity: 10})
	r.RemoveCenter("fc1")

	active := r.ActiveCenters()
	if len(active) != 1 || active[0].ID != "fc2" {
		t.Errorf("unexpected active centers after remove: %+v", active)
	}
}

func TestRouter_RemoveNotFound(t *testing.T) {
	t.Parallel()
	r := &Router{}
	// Should not panic on missing ID.
	r.RemoveCenter("ghost")
}

func TestRouteOrder_NearestCenter(t *testing.T) {
	t.Parallel()
	// Brisbane (-27.47, 153.02), Sydney (-33.87, 151.21), Melbourne (-37.81, 144.96)
	// destination near Brisbane
	dest := Location{Lat: -27.5, Lon: 153.0}
	centers := []FulfillmentCenter{
		{ID: "bris", Active: true, Capacity: 100, Location: Location{Lat: -27.47, Lon: 153.02}},
		{ID: "syd", Active: true, Capacity: 100, Location: Location{Lat: -33.87, Lon: 151.21}},
		{ID: "mel", Active: true, Capacity: 100, Location: Location{Lat: -37.81, Lon: 144.96}},
	}

	fc, err := RouteOrder(centers, dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc.ID != "bris" {
		t.Errorf("expected nearest center bris, got %s", fc.ID)
	}
}

func TestRouteOrder_InactiveCentersExcluded(t *testing.T) {
	t.Parallel()
	dest := Location{Lat: 0, Lon: 0}
	centers := []FulfillmentCenter{
		{ID: "near", Active: false, Capacity: 100, Location: Location{Lat: 0, Lon: 0}},
		{ID: "far", Active: true, Capacity: 100, Location: Location{Lat: 10, Lon: 10}},
	}

	fc, err := RouteOrder(centers, dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc.ID != "far" {
		t.Errorf("expected far (only active center), got %s", fc.ID)
	}
}

func TestRouteOrder_ZeroCapacityExcluded(t *testing.T) {
	t.Parallel()
	dest := Location{Lat: 0, Lon: 0}
	centers := []FulfillmentCenter{
		{ID: "full", Active: true, Capacity: 0, Location: Location{Lat: 0, Lon: 0}},
		{ID: "avail", Active: true, Capacity: 5, Location: Location{Lat: 5, Lon: 5}},
	}

	fc, err := RouteOrder(centers, dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc.ID != "avail" {
		t.Errorf("expected avail center, got %s", fc.ID)
	}
}

func TestRouteOrder_NoAvailableCenter(t *testing.T) {
	t.Parallel()
	dest := Location{Lat: 0, Lon: 0}
	centers := []FulfillmentCenter{
		{ID: "x", Active: false, Capacity: 100},
		{ID: "y", Active: true, Capacity: 0},
	}

	_, err := RouteOrder(centers, dest)
	if !errors.Is(err, ErrNoAvailableCenter) {
		t.Errorf("expected ErrNoAvailableCenter, got %v", err)
	}
}

func TestRouteOrder_EmptyCenters(t *testing.T) {
	t.Parallel()
	_, err := RouteOrder(nil, Location{})
	if !errors.Is(err, ErrNoAvailableCenter) {
		t.Errorf("expected ErrNoAvailableCenter for empty list, got %v", err)
	}
}

func TestCapacityManager_ReserveRelease(t *testing.T) {
	t.Parallel()
	cm := &CapacityManager{}
	cm.SetCapacity("c1", 100)

	if err := cm.Reserve("c1", 40); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rem := cm.Remaining("c1"); rem != 60 {
		t.Errorf("expected 60 remaining, got %d", rem)
	}

	cm.Release("c1", 40)
	if rem := cm.Remaining("c1"); rem != 100 {
		t.Errorf("expected 100 after release, got %d", rem)
	}
}

func TestCapacityManager_OverCapacity(t *testing.T) {
	t.Parallel()
	cm := &CapacityManager{}
	cm.SetCapacity("c2", 10)

	err := cm.Reserve("c2", 20)
	if !errors.Is(err, ErrInsufficientCap) {
		t.Errorf("expected ErrInsufficientCap, got %v", err)
	}
}

func TestCapacityManager_NotFound(t *testing.T) {
	t.Parallel()
	cm := &CapacityManager{}

	err := cm.Reserve("ghost", 1)
	if !errors.Is(err, ErrCenterNotFound) {
		t.Errorf("expected ErrCenterNotFound, got %v", err)
	}
}

func TestCapacityManager_ReleaseNotFound(t *testing.T) {
	t.Parallel()
	// Should not panic on unknown center.
	cm := &CapacityManager{}
	cm.Release("ghost", 5)
}

func TestCapacityManager_RemainingUnknown(t *testing.T) {
	t.Parallel()
	cm := &CapacityManager{}
	if r := cm.Remaining("unknown"); r != 0 {
		t.Errorf("expected 0 for unknown center, got %d", r)
	}
}

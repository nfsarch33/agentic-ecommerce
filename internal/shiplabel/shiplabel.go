package shiplabel

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors.
var (
	ErrNoRates          = errors.New("shiplabel: no rates available")
	ErrNoRatesWithinDays = errors.New("shiplabel: no rates available within day limit")
)

// RateRequest contains the shipment details needed to fetch carrier rates.
type RateRequest struct {
	FromZip   string
	ToZip     string
	Country   string
	WeightKg  float64
	LengthCm  float64
	WidthCm   float64
	HeightCm  float64
}

// Rate is a shipping rate quote from a carrier.
type Rate struct {
	Carrier       string
	Service       string
	PriceCents    int
	EstimatedDays int
}

// RateProvider is the interface for fetching shipping rates.
type RateProvider interface {
	GetRates(ctx context.Context, req RateRequest) ([]Rate, error)
}

// StubRateProvider returns a configurable slice of rates.
type StubRateProvider struct {
	Rates []Rate
	Err   error
}

// GetRates returns the configured rates (or error) regardless of the request.
func (s *StubRateProvider) GetRates(_ context.Context, _ RateRequest) ([]Rate, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	// Return a copy to prevent mutation.
	out := make([]Rate, len(s.Rates))
	copy(out, s.Rates)
	return out, nil
}

// RateComparator provides rate selection helpers.
type RateComparator struct{}

// CheapestRate returns the rate with the lowest PriceCents.
// Returns ErrNoRates if the slice is empty.
func (rc RateComparator) CheapestRate(rates []Rate) (*Rate, error) {
	if len(rates) == 0 {
		return nil, ErrNoRates
	}
	best := rates[0]
	for _, r := range rates[1:] {
		if r.PriceCents < best.PriceCents {
			best = r
		}
	}
	return &best, nil
}

// FastestRate returns the rate with the lowest EstimatedDays.
// Returns ErrNoRates if the slice is empty.
func (rc RateComparator) FastestRate(rates []Rate) (*Rate, error) {
	if len(rates) == 0 {
		return nil, ErrNoRates
	}
	best := rates[0]
	for _, r := range rates[1:] {
		if r.EstimatedDays < best.EstimatedDays {
			best = r
		}
	}
	return &best, nil
}

// BestValue returns the cheapest rate among those with EstimatedDays <= maxDays.
// Returns ErrNoRatesWithinDays if no rate meets the day limit, or ErrNoRates if
// the input slice is empty.
func (rc RateComparator) BestValue(rates []Rate, maxDays int) (*Rate, error) {
	if len(rates) == 0 {
		return nil, ErrNoRates
	}
	var candidates []Rate
	for _, r := range rates {
		if r.EstimatedDays <= maxDays {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: maxDays=%d", ErrNoRatesWithinDays, maxDays)
	}
	best := candidates[0]
	for _, r := range candidates[1:] {
		if r.PriceCents < best.PriceCents {
			best = r
		}
	}
	return &best, nil
}

// BatchPrint holds a collection of printed label bytes.
type BatchPrint struct {
	Labels    [][]byte
	PrintedAt time.Time
}

// PrintBatch collects the provided label byte slices into a BatchPrint record.
func PrintBatch(labels [][]byte) BatchPrint {
	out := make([][]byte, len(labels))
	for i, l := range labels {
		cp := make([]byte, len(l))
		copy(cp, l)
		out[i] = cp
	}
	return BatchPrint{
		Labels:    out,
		PrintedAt: time.Now(),
	}
}

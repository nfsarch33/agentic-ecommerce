package discount

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrCouponExpired      = errors.New("coupon expired")
	ErrMinOrderNotMet     = errors.New("minimum order not met")
	ErrStackingNotAllowed = errors.New("stacking not allowed: only one percentage coupon permitted")
	ErrCouponNotFound     = errors.New("coupon not found")
)

type CouponType string

const (
	TypePercentage  CouponType = "percentage"
	TypeFixed       CouponType = "fixed_amount"
	TypeFreeShipping CouponType = "free_shipping"
	TypeBuyXGetY    CouponType = "buy_x_get_y"
)

type Coupon struct {
	Code       string
	Type       CouponType
	Value      int
	ExpiresAt  time.Time
	MinOrder   int
	UsageLimit int
}

type Cart struct {
	Total int
}

type DiscountResult struct {
	OriginalTotal  int
	DiscountAmount int
	FinalTotal     int
	FreeShipping   bool
	AppliedCoupons []string
}

type DiscountEngine struct {
	mu      sync.RWMutex
	coupons map[string]Coupon
}

func NewDiscountEngine() *DiscountEngine {
	return &DiscountEngine{
		coupons: make(map[string]Coupon),
	}
}

func (e *DiscountEngine) AddCoupon(c Coupon) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.coupons[c.Code] = c
}

func (e *DiscountEngine) Apply(cart Cart, codes []string) (DiscountResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	now := time.Now()
	var resolved []Coupon
	for _, code := range codes {
		c, ok := e.coupons[code]
		if !ok {
			return DiscountResult{}, ErrCouponNotFound
		}
		if now.After(c.ExpiresAt) {
			return DiscountResult{}, ErrCouponExpired
		}
		if c.MinOrder > 0 && cart.Total < c.MinOrder {
			return DiscountResult{}, ErrMinOrderNotMet
		}
		resolved = append(resolved, c)
	}

	// Stacking rule: at most one percentage coupon.
	pctCount := 0
	for _, c := range resolved {
		if c.Type == TypePercentage {
			pctCount++
		}
	}
	if pctCount > 1 {
		return DiscountResult{}, ErrStackingNotAllowed
	}

	result := DiscountResult{
		OriginalTotal:  cart.Total,
		AppliedCoupons: codes,
	}

	for _, c := range resolved {
		switch c.Type {
		case TypePercentage:
			result.DiscountAmount += cart.Total * c.Value / 100
		case TypeFixed:
			result.DiscountAmount += c.Value
		case TypeFreeShipping:
			result.FreeShipping = true
		}
	}

	result.FinalTotal = cart.Total - result.DiscountAmount
	if result.FinalTotal < 0 {
		result.FinalTotal = 0
	}

	return result, nil
}

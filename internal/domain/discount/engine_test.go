package discount_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/domain/discount"
)

func makeCart(total int) discount.Cart {
	return discount.Cart{Total: total}
}

func expiry(d time.Duration) time.Time { return time.Now().Add(d) }

func TestApply_PercentageDiscount(t *testing.T) {
	t.Parallel()
	eng := discount.NewDiscountEngine()
	eng.AddCoupon(discount.Coupon{Code: "SAVE10", Type: discount.TypePercentage, Value: 10, ExpiresAt: expiry(time.Hour)})

	result, err := eng.Apply(makeCart(10000), []string{"SAVE10"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.DiscountAmount != 1000 {
		t.Fatalf("expected 1000 discount, got %d", result.DiscountAmount)
	}
	if result.FinalTotal != 9000 {
		t.Fatalf("expected 9000 final, got %d", result.FinalTotal)
	}
}

func TestApply_FixedAmount(t *testing.T) {
	t.Parallel()
	eng := discount.NewDiscountEngine()
	eng.AddCoupon(discount.Coupon{Code: "FLAT500", Type: discount.TypeFixed, Value: 500, ExpiresAt: expiry(time.Hour)})

	result, err := eng.Apply(makeCart(10000), []string{"FLAT500"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.DiscountAmount != 500 {
		t.Fatalf("expected 500 discount, got %d", result.DiscountAmount)
	}
}

func TestApply_FreeShipping(t *testing.T) {
	t.Parallel()
	eng := discount.NewDiscountEngine()
	eng.AddCoupon(discount.Coupon{Code: "FREESHIP", Type: discount.TypeFreeShipping, Value: 0, ExpiresAt: expiry(time.Hour)})

	result, err := eng.Apply(makeCart(5000), []string{"FREESHIP"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !result.FreeShipping {
		t.Fatal("expected free shipping flag")
	}
}

func TestApply_ExpiredCoupon(t *testing.T) {
	t.Parallel()
	eng := discount.NewDiscountEngine()
	eng.AddCoupon(discount.Coupon{Code: "OLD", Type: discount.TypeFixed, Value: 500, ExpiresAt: time.Now().Add(-time.Hour)})

	_, err := eng.Apply(makeCart(5000), []string{"OLD"})
	if !errors.Is(err, discount.ErrCouponExpired) {
		t.Fatalf("expected ErrCouponExpired, got %v", err)
	}
}

func TestApply_MinOrderNotMet(t *testing.T) {
	t.Parallel()
	eng := discount.NewDiscountEngine()
	eng.AddCoupon(discount.Coupon{Code: "MIN100", Type: discount.TypeFixed, Value: 500, MinOrder: 10000, ExpiresAt: expiry(time.Hour)})

	_, err := eng.Apply(makeCart(5000), []string{"MIN100"})
	if !errors.Is(err, discount.ErrMinOrderNotMet) {
		t.Fatalf("expected ErrMinOrderNotMet, got %v", err)
	}
}

func TestApply_StackingRules_TwoPercentagesFail(t *testing.T) {
	t.Parallel()
	eng := discount.NewDiscountEngine()
	eng.AddCoupon(discount.Coupon{Code: "P10", Type: discount.TypePercentage, Value: 10, ExpiresAt: expiry(time.Hour)})
	eng.AddCoupon(discount.Coupon{Code: "P20", Type: discount.TypePercentage, Value: 20, ExpiresAt: expiry(time.Hour)})

	_, err := eng.Apply(makeCart(10000), []string{"P10", "P20"})
	if !errors.Is(err, discount.ErrStackingNotAllowed) {
		t.Fatalf("expected ErrStackingNotAllowed, got %v", err)
	}
}

func TestApply_FixedCanStack(t *testing.T) {
	t.Parallel()
	eng := discount.NewDiscountEngine()
	eng.AddCoupon(discount.Coupon{Code: "F100", Type: discount.TypeFixed, Value: 100, ExpiresAt: expiry(time.Hour)})
	eng.AddCoupon(discount.Coupon{Code: "F200", Type: discount.TypeFixed, Value: 200, ExpiresAt: expiry(time.Hour)})

	result, err := eng.Apply(makeCart(10000), []string{"F100", "F200"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.DiscountAmount != 300 {
		t.Fatalf("expected 300 stacked discount, got %d", result.DiscountAmount)
	}
}

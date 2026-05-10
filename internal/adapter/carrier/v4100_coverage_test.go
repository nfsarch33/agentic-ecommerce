package carrier

import (
	"testing"
	"time"
)

func TestAusPostClient_Name(t *testing.T) {
	t.Parallel()
	c := &AusPostClient{}
	if got := c.Name(); got != CarrierAusPost {
		t.Fatalf("Name()=%q want %s", got, CarrierAusPost)
	}
}

func TestDHLClient_Name(t *testing.T) {
	t.Parallel()
	c := &DHLClient{}
	if got := c.Name(); got != CarrierDHL {
		t.Fatalf("Name()=%q want %s", got, CarrierDHL)
	}
}

func TestKeyRotator_NotExpired(t *testing.T) {
	t.Parallel()
	r, err := NewKeyRotator(KeyRotationConfig{
		CarrierName: "test",
		CurrentKey:  "primary-key",
		TTL:         24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewKeyRotator: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil rotator")
	}
}

func TestKeyRotator_MissingCurrentKey(t *testing.T) {
	t.Parallel()
	_, err := NewKeyRotator(KeyRotationConfig{CarrierName: "test"})
	if err == nil {
		t.Fatal("expected error for missing current key")
	}
}

func TestValidateQuoteRequest_EmptyOriginPost(t *testing.T) {
	t.Parallel()
	err := validateQuoteRequest(QuoteRequest{
		DestPost: "3000", WeightGrams: 500,
	})
	if err == nil {
		t.Fatal("expected error for empty origin")
	}
}

func TestValidateQuoteRequest_EmptyDestPost(t *testing.T) {
	t.Parallel()
	err := validateQuoteRequest(QuoteRequest{
		OriginPost: "2000", WeightGrams: 500,
	})
	if err == nil {
		t.Fatal("expected error for empty dest")
	}
}

func TestValidateQuoteRequest_ZeroWeight(t *testing.T) {
	t.Parallel()
	err := validateQuoteRequest(QuoteRequest{
		OriginPost: "2000", DestPost: "3000",
	})
	if err == nil {
		t.Fatal("expected error for zero weight")
	}
}

func TestValidateLabelRequest_EmptyOrderID(t *testing.T) {
	t.Parallel()
	err := validateLabelRequest(LabelRequest{
		DestPost: "3000", WeightGrams: 500,
	})
	if err == nil {
		t.Fatal("expected error for empty order ID")
	}
}

func TestValidateLabelRequest_EmptyDestPost(t *testing.T) {
	t.Parallel()
	err := validateLabelRequest(LabelRequest{
		OrderID: "o1", WeightGrams: 500,
	})
	if err == nil {
		t.Fatal("expected error for empty dest post")
	}
}

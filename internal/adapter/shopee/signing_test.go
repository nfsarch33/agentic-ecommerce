package shopee

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
)

const testPartnerKey = "shopee-partner-test-key-32-bytes" // gitleaks:allow

func TestComputeSignatureUsesV2ShopScopedCanonicalForm(t *testing.T) {
	t.Parallel()

	req := SignRequest{
		PartnerKey:  []byte(testPartnerKey),
		PartnerID:   123456,
		Path:        "/api/v2/product/add_item",
		Timestamp:   1777777777,
		AccessToken: "test-access-token",
		ShopID:      987654,
	}

	got, err := ComputeSignature(req)
	if err != nil {
		t.Fatalf("ComputeSignature: %v", err)
	}
	want := referenceShopeeSignature(req)
	if got != want {
		t.Fatalf("signature mismatch:\n got  %s\n want %s", got, want)
	}
}

func TestVerifySignatureRejectsTamperedHex(t *testing.T) {
	t.Parallel()

	req := SignRequest{
		PartnerKey:  []byte(testPartnerKey),
		PartnerID:   123456,
		Path:        "/api/v2/product/update_item",
		Timestamp:   1777777788,
		AccessToken: "test-access-token",
		ShopID:      987654,
	}
	good, err := ComputeSignature(req)
	if err != nil {
		t.Fatalf("ComputeSignature: %v", err)
	}

	err = VerifySignature(req, flipLastHexChar(good))
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("err = %v, want ErrSignatureMismatch", err)
	}
}

func TestComputeSignatureRejectsIncompleteInputs(t *testing.T) {
	t.Parallel()

	_, err := ComputeSignature(SignRequest{
		PartnerKey: []byte("short"),
		PartnerID:  123456,
		Path:       "/api/v2/product/add_item",
		Timestamp:  1777777777,
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func referenceShopeeSignature(req SignRequest) string {
	base := strconv.FormatInt(req.PartnerID, 10) +
		req.Path +
		strconv.FormatInt(req.Timestamp, 10) +
		req.AccessToken +
		strconv.FormatInt(req.ShopID, 10)
	mac := hmac.New(sha256.New, req.PartnerKey)
	_, _ = mac.Write([]byte(base))
	return hex.EncodeToString(mac.Sum(nil))
}

func flipLastHexChar(value string) string {
	if value == "" {
		return "0"
	}
	last := value[len(value)-1]
	replacement := byte('0')
	if last == '0' {
		replacement = '1'
	}
	return value[:len(value)-1] + string(replacement)
}

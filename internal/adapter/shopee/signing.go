package shopee

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const minPartnerKeyBytes = 16

var (
	ErrInvalidConfig     = errors.New("shopee: invalid config")
	ErrSignatureMismatch = errors.New("shopee: signature mismatch")
)

type SignRequest struct {
	PartnerKey  []byte
	PartnerID   int64
	Path        string
	Timestamp   int64
	AccessToken string
	ShopID      int64
}

func ComputeSignature(req SignRequest) (string, error) {
	if err := validateSignRequest(req); err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, req.PartnerKey)
	_, _ = mac.Write([]byte(req.baseString()))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func VerifySignature(req SignRequest, supplied string) error {
	want, err := ComputeSignature(req)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(want), []byte(strings.TrimSpace(supplied))) == 1 {
		return nil
	}
	return ErrSignatureMismatch
}

func validateSignRequest(req SignRequest) error {
	switch {
	case len(req.PartnerKey) < minPartnerKeyBytes:
		return fmt.Errorf("%w: partner key too short", ErrInvalidConfig)
	case req.PartnerID <= 0:
		return fmt.Errorf("%w: partner id required", ErrInvalidConfig)
	case strings.TrimSpace(req.Path) == "":
		return fmt.Errorf("%w: api path required", ErrInvalidConfig)
	case req.Timestamp <= 0:
		return fmt.Errorf("%w: timestamp required", ErrInvalidConfig)
	default:
		return nil
	}
}

func (req SignRequest) baseString() string {
	return strconv.FormatInt(req.PartnerID, 10) +
		req.Path +
		strconv.FormatInt(req.Timestamp, 10) +
		req.AccessToken +
		strconv.FormatInt(req.ShopID, 10)
}

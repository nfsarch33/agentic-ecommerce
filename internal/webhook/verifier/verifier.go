// Package verifier provides a shared webhook verification interface
// and concrete implementations for HMAC, RSA, and AEAD algorithms.
// Extracted in v5.3.0 to deduplicate verify-then-parse patterns across
// TikTok, Facebook, AusPost, DHL, Stripe, Alipay, WeChat, PayPal,
// and Pinterest webhook handlers.
package verifier

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"net/http"
)

// Verifier checks the authenticity of a webhook request. Each provider
// implements this with its own signature algorithm.
type Verifier interface {
	Verify(headers http.Header, body []byte) error
}

// VerifyAndParse verifies the webhook signature, then unmarshals
// the body into T. Generic function to reduce boilerplate in all
// webhook handlers.
func VerifyAndParse[T any](ctx context.Context, headers http.Header, body []byte, v Verifier) (T, error) {
	_ = ctx
	var zero T
	if err := v.Verify(headers, body); err != nil {
		return zero, fmt.Errorf("webhook verify: %w", err)
	}
	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return zero, fmt.Errorf("webhook parse: %w", err)
	}
	return result, nil
}

// HMACVerifier verifies HMAC signatures. Covers Stripe (HMAC-SHA256
// with t=...,v1=... format), TikTok, Facebook X-Hub-Signature-256,
// AusPost, DHL, and Pinterest patterns.
type HMACVerifier struct {
	Algorithm   func() hash.Hash
	Secret      []byte
	HeaderName  string
	SignatureFn func(headers http.Header, body []byte, secret []byte, alg func() hash.Hash) (expected, actual string, err error)
}

// NewHMACSHA256Verifier creates an HMAC-SHA256 verifier with a custom
// signature extraction function. The SignatureFn receives headers,
// body, secret, and algorithm, and returns the expected and actual
// hex signatures for comparison.
func NewHMACSHA256Verifier(secret []byte, headerName string, sigFn func(http.Header, []byte, []byte, func() hash.Hash) (string, string, error)) *HMACVerifier {
	return &HMACVerifier{
		Algorithm:   sha256.New,
		Secret:      secret,
		HeaderName:  headerName,
		SignatureFn: sigFn,
	}
}

// Verify computes the expected HMAC and compares it against the
// signature from the request header.
func (v *HMACVerifier) Verify(headers http.Header, body []byte) error {
	if v.SignatureFn != nil {
		expected, actual, err := v.SignatureFn(headers, body, v.Secret, v.Algorithm)
		if err != nil {
			return err
		}
		if !hmac.Equal([]byte(expected), []byte(actual)) {
			return fmt.Errorf("hmac: signature mismatch")
		}
		return nil
	}
	sig := headers.Get(v.HeaderName)
	if sig == "" {
		return fmt.Errorf("hmac: missing header %s", v.HeaderName)
	}
	mac := hmac.New(v.Algorithm, v.Secret)
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return fmt.Errorf("hmac: signature mismatch")
	}
	return nil
}

// XHubSignatureVerifier verifies the X-Hub-Signature-256 header
// used by Facebook/Meta webhooks (sha256=<hex>).
type XHubSignatureVerifier struct {
	Secret []byte
}

// Verify checks the X-Hub-Signature-256 header.
func (v *XHubSignatureVerifier) Verify(headers http.Header, body []byte) error {
	sig := headers.Get("X-Hub-Signature-256")
	if sig == "" {
		return fmt.Errorf("xhub: missing X-Hub-Signature-256 header")
	}
	if len(sig) < 7 || sig[:7] != "sha256=" {
		return fmt.Errorf("xhub: invalid signature format")
	}
	mac := hmac.New(sha256.New, v.Secret)
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return fmt.Errorf("xhub: signature mismatch")
	}
	return nil
}

// RSAVerifier verifies RSA-SHA256 signatures, used by Alipay.
type RSAVerifier struct {
	PublicKey *rsa.PublicKey
	BuildMsg  func(headers http.Header, body []byte) (message []byte, signature []byte, err error)
}

// Verify checks the RSA-SHA256 signature.
func (v *RSAVerifier) Verify(headers http.Header, body []byte) error {
	msg, sig, err := v.BuildMsg(headers, body)
	if err != nil {
		return fmt.Errorf("rsa: %w", err)
	}
	hashed := sha256.Sum256(msg)
	if err := rsa.VerifyPKCS1v15(v.PublicKey, crypto.SHA256, hashed[:], sig); err != nil {
		return fmt.Errorf("rsa: verification failed: %w", err)
	}
	return nil
}

// AEADVerifier decrypts and verifies AEAD-AES-256-GCM payloads,
// used by WeChat Pay API v3.
type AEADVerifier struct {
	Key       []byte
	ExtractFn func(headers http.Header, body []byte) (nonce, ciphertext, aad string, err error)
}

// Verify decrypts the AEAD envelope and validates integrity.
func (v *AEADVerifier) Verify(headers http.Header, body []byte) error {
	nonce, ciphertextB64, aad, err := v.ExtractFn(headers, body)
	if err != nil {
		return fmt.Errorf("aead: extract: %w", err)
	}
	_, err = DecryptAEAD(v.Key, nonce, ciphertextB64, aad)
	if err != nil {
		return fmt.Errorf("aead: %w", err)
	}
	return nil
}

// DecryptAEAD performs AES-256-GCM decryption. Exported so callers
// that need the plaintext (e.g. WeChat webhook handler) can use it.
func DecryptAEAD(key []byte, nonceStr, ciphertextB64, aad string) ([]byte, error) {
	derivedKey := deriveAESKey(key)
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	plaintext, err := gcm.Open(nil, []byte(nonceStr), ciphertext, []byte(aad))
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

func deriveAESKey(key []byte) []byte {
	if len(key) == 32 {
		return key
	}
	h := sha256.Sum256(key)
	return h[:]
}

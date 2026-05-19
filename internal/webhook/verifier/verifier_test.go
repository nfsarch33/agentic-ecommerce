package verifier_test

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"net/http"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/webhook/verifier"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHMACVerifier_SHA256_Valid(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-key-32bytes-long!!")
	body := []byte(`{"event":"test"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	v := &verifier.HMACVerifier{
		Algorithm:  sha256.New,
		Secret:     secret,
		HeaderName: "X-Signature",
	}
	headers := http.Header{}
	headers.Set("X-Signature", sig)
	err := v.Verify(headers, body)
	require.NoError(t, err)
}

func TestHMACVerifier_SHA256_Invalid(t *testing.T) {
	t.Parallel()
	v := &verifier.HMACVerifier{
		Algorithm:  sha256.New,
		Secret:     []byte("secret"),
		HeaderName: "X-Signature",
	}
	headers := http.Header{}
	headers.Set("X-Signature", "badsig")
	err := v.Verify(headers, []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mismatch")
}

func TestHMACVerifier_MissingHeader(t *testing.T) {
	t.Parallel()
	v := &verifier.HMACVerifier{
		Algorithm:  sha256.New,
		Secret:     []byte("secret"),
		HeaderName: "X-Signature",
	}
	err := v.Verify(http.Header{}, []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing header")
}

func TestHMACVerifier_CustomSignatureFn(t *testing.T) {
	t.Parallel()
	secret := []byte("stripe-secret-32-bytes-long!!!!")
	body := []byte(`{"id":"evt_1"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("12345."))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	sigFn := func(headers http.Header, body, sec []byte, alg func() hash.Hash) (string, string, error) {
		sig := headers.Get("Stripe-Signature")
		m := hmac.New(alg, sec)
		m.Write([]byte("12345."))
		m.Write(body)
		return hex.EncodeToString(m.Sum(nil)), sig, nil
	}
	v := verifier.NewHMACSHA256Verifier(secret, "Stripe-Signature", sigFn)
	headers := http.Header{}
	headers.Set("Stripe-Signature", expected)
	err := v.Verify(headers, body)
	require.NoError(t, err)
}

func TestXHubSignatureVerifier_Valid(t *testing.T) {
	t.Parallel()
	secret := []byte("fb-secret")
	body := []byte(`{"object":"page"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	v := &verifier.XHubSignatureVerifier{Secret: secret}
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", sig)
	err := v.Verify(headers, body)
	require.NoError(t, err)
}

func TestXHubSignatureVerifier_Invalid(t *testing.T) {
	t.Parallel()
	v := &verifier.XHubSignatureVerifier{Secret: []byte("secret")}
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", "sha256=badsig")
	err := v.Verify(headers, []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mismatch")
}

func TestXHubSignatureVerifier_Missing(t *testing.T) {
	t.Parallel()
	v := &verifier.XHubSignatureVerifier{Secret: []byte("secret")}
	err := v.Verify(http.Header{}, []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestRSAVerifier_SHA256_Valid(t *testing.T) {
	t.Parallel()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	msg := []byte("app_id=test&trade_no=123")
	hashed := sha256.Sum256(msg)
	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hashed[:])
	require.NoError(t, err)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	v := &verifier.RSAVerifier{
		PublicKey: &privKey.PublicKey,
		BuildMsg: func(_ http.Header, body []byte) ([]byte, []byte, error) {
			sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
			return body, sigBytes, err
		},
	}
	err = v.Verify(http.Header{}, msg)
	require.NoError(t, err)
}

func TestRSAVerifier_SHA256_Invalid(t *testing.T) {
	t.Parallel()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	v := &verifier.RSAVerifier{
		PublicKey: &privKey.PublicKey,
		BuildMsg: func(_ http.Header, _ []byte) ([]byte, []byte, error) {
			return []byte("msg"), []byte("bad-sig"), nil
		},
	}
	err = v.Verify(http.Header{}, []byte("msg"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification failed")
}

func TestAEADVerifier_GCM_Valid(t *testing.T) {
	t.Parallel()
	key := make([]byte, 32)
	_, _ = rand.Read(key)

	plaintext := []byte(`{"transaction_id":"tx_123"}`)
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	aad := "transaction"

	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(aad))
	ciphertextB64 := base64.StdEncoding.EncodeToString(ciphertext)
	nonceStr := string(nonce)

	v := &verifier.AEADVerifier{
		Key: key,
		ExtractFn: func(_ http.Header, _ []byte) (string, string, string, error) {
			return nonceStr, ciphertextB64, aad, nil
		},
	}
	err = v.Verify(http.Header{}, []byte(`{}`))
	require.NoError(t, err)
}

func TestAEADVerifier_GCM_Invalid(t *testing.T) {
	t.Parallel()
	key := make([]byte, 32)
	_, _ = rand.Read(key)

	v := &verifier.AEADVerifier{
		Key: key,
		ExtractFn: func(_ http.Header, _ []byte) (string, string, string, error) {
			return "123456789012", base64.StdEncoding.EncodeToString([]byte("bad")), "aad", nil
		},
	}
	err := v.Verify(http.Header{}, []byte(`{}`))
	require.Error(t, err)
}

func TestDecryptAEAD_Success(t *testing.T) {
	t.Parallel()
	key := make([]byte, 32)
	_, _ = rand.Read(key)

	plaintext := []byte(`{"status":"ok"}`)
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	aad := "test"

	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	ct := gcm.Seal(nil, nonce, plaintext, []byte(aad))

	result, err := verifier.DecryptAEAD(key, string(nonce), base64.StdEncoding.EncodeToString(ct), aad)
	require.NoError(t, err)
	assert.Equal(t, plaintext, result)
}

func TestVerifyAndParse_Success(t *testing.T) {
	t.Parallel()
	type Event struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	secret := []byte("secret")
	body := []byte(`{"id":"evt_1","type":"charge.succeeded"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	v := &verifier.HMACVerifier{
		Algorithm:  sha256.New,
		Secret:     secret,
		HeaderName: "X-Sig",
	}
	headers := http.Header{}
	headers.Set("X-Sig", sig)

	evt, err := verifier.VerifyAndParse[Event](context.Background(), headers, body, v)
	require.NoError(t, err)
	assert.Equal(t, "evt_1", evt.ID)
	assert.Equal(t, "charge.succeeded", evt.Type)
}

func TestVerifyAndParse_VerifyFails(t *testing.T) {
	t.Parallel()
	type Event struct{}
	v := &verifier.HMACVerifier{
		Algorithm:  sha256.New,
		Secret:     []byte("s"),
		HeaderName: "X-Sig",
	}
	headers := http.Header{}
	headers.Set("X-Sig", "bad")
	_, err := verifier.VerifyAndParse[Event](context.Background(), headers, []byte(`{}`), v)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verify")
}

func init() {
	_ = fmt.Sprintf
	_ = json.Marshal
}

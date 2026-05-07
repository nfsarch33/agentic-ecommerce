package outbound

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"
)

type Signer struct {
	secret string
}

func NewSigner(secret string) Signer {
	return Signer{secret: secret}
}

func (s Signer) Sign(webhookID string, timestamp time.Time, body []byte) http.Header {
	unix := timestamp.UTC().Unix()
	mac := hmac.New(sha256.New, []byte(s.secret))
	_, _ = mac.Write([]byte(strconv.FormatInt(unix, 10)))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)

	headers := http.Header{}
	headers.Set("X-EC-Webhook-ID", webhookID)
	headers.Set("X-EC-Webhook-Timestamp", strconv.FormatInt(unix, 10))
	headers.Set("X-EC-Webhook-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	return headers
}

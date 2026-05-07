package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

type Principal struct {
	Subject string
	Role    Role
}

type TokenConfig struct {
	Secret    string
	Issuer    string
	Audience  string
	AccessTTL time.Duration
	Now       func() time.Time
}

type AccessClaims struct {
	Subject   string
	Role      Role
	Issuer    string
	Audience  string
	IssuedAt  time.Time
	NotBefore time.Time
	ExpiresAt time.Time
	ID        string
}

type TokenManager struct {
	secret    []byte
	issuer    string
	audience  string
	accessTTL time.Duration
	now       func() time.Time
}

type jwtHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

type jwtClaims struct {
	Subject   string `json:"sub"`
	Role      string `json:"role"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	IssuedAt  int64  `json:"iat"`
	NotBefore int64  `json:"nbf"`
	ExpiresAt int64  `json:"exp"`
	ID        string `json:"jti"`
}

func NewTokenManager(cfg TokenConfig) (*TokenManager, error) {
	secret := strings.TrimSpace(cfg.Secret)
	if len(secret) < 32 {
		return nil, fmt.Errorf("jwt secret must be at least 32 bytes")
	}
	ttl := cfg.AccessTTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	issuer := strings.TrimSpace(cfg.Issuer)
	if issuer == "" {
		issuer = "agentic-ecommerce"
	}
	audience := strings.TrimSpace(cfg.Audience)
	if audience == "" {
		audience = "mc-api"
	}
	return &TokenManager{
		secret:    []byte(secret),
		issuer:    issuer,
		audience:  audience,
		accessTTL: ttl,
		now:       now,
	}, nil
}

func (m *TokenManager) MintAccessToken(principal Principal) (string, error) {
	if m == nil {
		return "", ErrInvalidToken
	}
	subject := strings.TrimSpace(principal.Subject)
	if subject == "" {
		return "", fmt.Errorf("subject required")
	}
	role, err := ParseRole(string(principal.Role))
	if err != nil {
		return "", err
	}
	now := m.now().UTC()
	id, err := randomID()
	if err != nil {
		return "", err
	}
	header := jwtHeader{Algorithm: "HS256", Type: "JWT"}
	claims := jwtClaims{
		Subject:   subject,
		Role:      string(role),
		Issuer:    m.issuer,
		Audience:  m.audience,
		IssuedAt:  now.Unix(),
		NotBefore: now.Unix(),
		ExpiresAt: now.Add(m.accessTTL).Unix(),
		ID:        id,
	}
	return m.sign(header, claims)
}

func (m *TokenManager) VerifyAccessToken(token string) (AccessClaims, error) {
	if m == nil {
		return AccessClaims{}, ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AccessClaims{}, ErrInvalidToken
	}
	signingInput := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return AccessClaims{}, ErrInvalidToken
	}
	expected := m.mac([]byte(signingInput))
	if subtle.ConstantTimeCompare(signature, expected) != 1 {
		return AccessClaims{}, ErrInvalidToken
	}

	var header jwtHeader
	if err := decodeSegment(parts[0], &header); err != nil {
		return AccessClaims{}, ErrInvalidToken
	}
	if header.Algorithm != "HS256" || header.Type != "JWT" {
		return AccessClaims{}, ErrInvalidToken
	}

	var raw jwtClaims
	if err := decodeSegment(parts[1], &raw); err != nil {
		return AccessClaims{}, ErrInvalidToken
	}
	if raw.Issuer != m.issuer || raw.Audience != m.audience || strings.TrimSpace(raw.Subject) == "" {
		return AccessClaims{}, ErrInvalidToken
	}
	role, err := ParseRole(raw.Role)
	if err != nil {
		return AccessClaims{}, ErrInvalidToken
	}
	now := m.now().UTC().Unix()
	if raw.NotBefore > now {
		return AccessClaims{}, ErrInvalidToken
	}
	if raw.ExpiresAt <= now {
		return AccessClaims{}, ErrExpiredToken
	}
	return AccessClaims{
		Subject:   raw.Subject,
		Role:      role,
		Issuer:    raw.Issuer,
		Audience:  raw.Audience,
		IssuedAt:  time.Unix(raw.IssuedAt, 0).UTC(),
		NotBefore: time.Unix(raw.NotBefore, 0).UTC(),
		ExpiresAt: time.Unix(raw.ExpiresAt, 0).UTC(),
		ID:        raw.ID,
	}, nil
}

func (m *TokenManager) AccessTTL() time.Duration {
	if m == nil {
		return 0
	}
	return m.accessTTL
}

func (m *TokenManager) sign(header jwtHeader, claims jwtClaims) (string, error) {
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	signature := m.mac([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (m *TokenManager) mac(input []byte) []byte {
	h := hmac.New(sha256.New, m.secret)
	_, _ = h.Write(input)
	return h.Sum(nil)
}

func decodeSegment(segment string, out any) error {
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func randomID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

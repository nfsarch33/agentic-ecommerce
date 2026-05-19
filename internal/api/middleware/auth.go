package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

var (
	ErrMissingToken    = errors.New("missing authorization token")
	ErrInvalidToken    = errors.New("invalid token")
	ErrTokenExpired    = errors.New("token expired")
	ErrInsufficientRole = errors.New("insufficient role")
)

// Claims holds the JWT payload.
type Claims struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Roles     []string  `json:"roles"`
	ExpiresAt time.Time `json:"expires_at"`
}

type contextKey string

const claimsKey contextKey = "claims"

// JWTValidator signs and validates HS256 JWTs (no external deps -- pure HMAC-SHA256).
type JWTValidator struct {
	secret []byte
}

func NewJWTValidator(secret []byte) *JWTValidator {
	return &JWTValidator{secret: secret}
}

// Sign creates a signed JWT token string from claims.
func (v *JWTValidator) Sign(c Claims) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	b64Payload := base64.RawURLEncoding.EncodeToString(payload)
	sig := v.sign(header + "." + b64Payload)
	return header + "." + b64Payload + "." + sig, nil
}

// Validate parses and verifies the token, returning Claims or an error.
func (v *JWTValidator) Validate(tokenString string) (*Claims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	expectedSig := v.sign(parts[0] + "." + parts[1])
	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, ErrInvalidToken
	}
	if time.Now().After(c.ExpiresAt) {
		return nil, ErrTokenExpired
	}
	return &c, nil
}

func (v *JWTValidator) sign(data string) string {
	mac := hmac.New(sha256.New, v.secret)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// AuthMiddleware provides Authenticate and RBAC middleware.
type AuthMiddleware struct {
	validator *JWTValidator
}

func NewAuthMiddleware(secret []byte) *AuthMiddleware {
	return &AuthMiddleware{validator: NewJWTValidator(secret)}
}

// Authenticate extracts and validates the Bearer token; injects claims into context.
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearerToken(r)
		if token == "" {
			http.Error(w, ErrMissingToken.Error(), http.StatusUnauthorized)
			return
		}
		claims, err := m.validator.Validate(token)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Require returns a middleware that enforces the given role.
func (m *AuthMiddleware) Require(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(claimsKey).(*Claims)
			if !ok || claims == nil || !hasRole(claims.Roles, role) {
				http.Error(w, ErrInsufficientRole.Error(), http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

func hasRole(roles []string, target string) bool {
	for _, r := range roles {
		if r == target {
			return true
		}
	}
	return false
}

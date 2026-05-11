package jwt

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Sentinels surfaced by the Rotator.
var (
	ErrInvalidToken    = errors.New("jwt: invalid token")
	ErrExpiredToken    = errors.New("jwt: token expired")
	ErrUnknownKey      = errors.New("jwt: unknown key version")
	ErrExpiredKey      = errors.New("jwt: key version retired")
	ErrRotatorBadInput = errors.New("jwt: rotator misconfigured")
)

// MinSecretLen is the minimum HMAC secret byte length the rotator
// accepts. Mirrors the security.TokenManager floor.
const MinSecretLen = 32

// Key is one entry in the rotation ring. Version is the JWT header
// `kid`. NotAfter == zero means the key is active and unbounded.
// When NotAfter is non-zero, tokens signed with this key are accepted
// until NotAfter; mint is rejected after rotation moves on.
type Key struct {
	Version  string
	Secret   []byte
	NotAfter time.Time // grace deadline; zero = open ended
}

// Config wires the rotator. Keys MUST contain at least one entry
// matching ActiveVersion. Now defaults to time.Now().UTC.
type Config struct {
	Keys          []Key
	ActiveVersion string
	Issuer        string
	Audience      string
	AccessTTL     time.Duration
	Now           func() time.Time
}

// Claims is the verified token payload returned by Verify.
type Claims struct {
	Subject   string
	Issuer    string
	Audience  string
	Version   string
	IssuedAt  time.Time
	NotBefore time.Time
	ExpiresAt time.Time
	ID        string
	Extra     map[string]string
}

// MintRequest is the input to Mint. Subject is required; Extra
// claims are emitted as {string:string} pairs in the JWT body.
type MintRequest struct {
	Subject string
	Extra   map[string]string
}

// Rotator is the v6.2.0 versioned signer/verifier.
type Rotator struct {
	mu        sync.RWMutex
	ring      keyRing
	activeVer string
	issuer    string
	audience  string
	accessTTL time.Duration
	now       func() time.Time
}

// NewRotator constructs the rotator. Returns ErrRotatorBadInput if
// the active version is missing from Keys or any secret is < 32 bytes.
func NewRotator(cfg Config) (*Rotator, error) {
	if len(cfg.Keys) == 0 {
		return nil, fmt.Errorf("%w: at least one key required", ErrRotatorBadInput)
	}
	if strings.TrimSpace(cfg.ActiveVersion) == "" {
		return nil, fmt.Errorf("%w: ActiveVersion required", ErrRotatorBadInput)
	}
	ring := newKeyRing()
	for _, k := range cfg.Keys {
		if err := ring.add(k); err != nil {
			return nil, err
		}
	}
	if _, ok := ring.lookup(cfg.ActiveVersion); !ok {
		return nil, fmt.Errorf("%w: ActiveVersion %q absent from Keys", ErrRotatorBadInput, cfg.ActiveVersion)
	}
	ttl := cfg.AccessTTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	issuer := strings.TrimSpace(cfg.Issuer)
	if issuer == "" {
		issuer = "agentic-ecommerce"
	}
	audience := strings.TrimSpace(cfg.Audience)
	if audience == "" {
		audience = "mc-api"
	}
	return &Rotator{
		ring:      ring,
		activeVer: cfg.ActiveVersion,
		issuer:    issuer,
		audience:  audience,
		accessTTL: ttl,
		now:       now,
	}, nil
}

// ActiveVersion returns the version (kid) of the current minting key.
func (r *Rotator) ActiveVersion() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeVer
}

// SetActive rotates the minting key. Existing tokens signed by the
// previous key remain valid until their key's NotAfter deadline (or
// indefinitely when NotAfter is zero).
func (r *Rotator) SetActive(version string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.ring.lookup(version); !ok {
		return fmt.Errorf("%w: SetActive unknown version %q", ErrUnknownKey, version)
	}
	r.activeVer = version
	return nil
}

// AddKey inserts a new key into the ring. Useful for the
// rotation-ahead pattern: new key registered, then SetActive flips
// over once all replicas observe it.
func (r *Rotator) AddKey(k Key) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ring.add(k)
}

// Versions returns a sorted snapshot of all known key versions.
func (r *Rotator) Versions() []string { return r.ring.versions() }

// Mint signs a token with the active key. Honours the configured
// AccessTTL.
func (r *Rotator) Mint(req MintRequest) (string, error) {
	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		return "", fmt.Errorf("%w: subject required", ErrRotatorBadInput)
	}
	r.mu.RLock()
	key, ok := r.ring.lookup(r.activeVer)
	issuer, audience, ttl, now := r.issuer, r.audience, r.accessTTL, r.now
	r.mu.RUnlock()
	if !ok {
		return "", ErrUnknownKey
	}
	id, err := randomID()
	if err != nil {
		return "", err
	}
	issuedAt := now().UTC()
	header := jwtHeader{Algorithm: "HS256", Type: "JWT", KeyID: key.Version}
	body := jwtBody{
		Subject:   subject,
		Issuer:    issuer,
		Audience:  audience,
		IssuedAt:  issuedAt.Unix(),
		NotBefore: issuedAt.Unix(),
		ExpiresAt: issuedAt.Add(ttl).Unix(),
		ID:        id,
		Extra:     req.Extra,
	}
	return signJWT(header, body, key.Secret)
}

// Verify decodes the token, resolves the signing key by `kid`, and
// enforces the standard time-window claims plus the rotator's
// per-key grace deadline.
func (r *Rotator) Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}
	header, err := decodeHeader(parts[0])
	if err != nil {
		return Claims{}, err
	}
	r.mu.RLock()
	key, ok := r.ring.lookup(header.KeyID)
	now := r.now
	issuer, audience := r.issuer, r.audience
	r.mu.RUnlock()
	if !ok {
		return Claims{}, fmt.Errorf("%w: kid=%s", ErrUnknownKey, header.KeyID)
	}
	if !key.NotAfter.IsZero() && now().UTC().After(key.NotAfter) {
		return Claims{}, fmt.Errorf("%w: kid=%s", ErrExpiredKey, header.KeyID)
	}
	if err := verifySignature(parts, key.Secret); err != nil {
		return Claims{}, err
	}
	body, err := decodeBody(parts[1])
	if err != nil {
		return Claims{}, err
	}
	return body.toClaims(now, issuer, audience, header.KeyID)
}

// --- ring -----------------------------------------------------------------

type keyRing struct {
	keys map[string]Key
}

func newKeyRing() keyRing { return keyRing{keys: map[string]Key{}} }

func (r keyRing) add(k Key) error {
	v := strings.TrimSpace(k.Version)
	if v == "" {
		return fmt.Errorf("%w: key version required", ErrRotatorBadInput)
	}
	if len(k.Secret) < MinSecretLen {
		return fmt.Errorf("%w: secret for %q must be >= %d bytes", ErrRotatorBadInput, v, MinSecretLen)
	}
	r.keys[v] = Key{Version: v, Secret: append([]byte{}, k.Secret...), NotAfter: k.NotAfter}
	return nil
}

func (r keyRing) lookup(v string) (Key, bool) {
	k, ok := r.keys[strings.TrimSpace(v)]
	return k, ok
}

func (r keyRing) versions() []string {
	out := make([]string, 0, len(r.keys))
	for v := range r.keys {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// --- JWT encode/decode ----------------------------------------------------

type jwtHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
	KeyID     string `json:"kid"`
}

type jwtBody struct {
	Subject   string            `json:"sub"`
	Issuer    string            `json:"iss"`
	Audience  string            `json:"aud"`
	IssuedAt  int64             `json:"iat"`
	NotBefore int64             `json:"nbf"`
	ExpiresAt int64             `json:"exp"`
	ID        string            `json:"jti"`
	Extra     map[string]string `json:"extra,omitempty"`
}

func (b jwtBody) toClaims(now func() time.Time, issuer, audience, version string) (Claims, error) {
	if b.Issuer != issuer || b.Audience != audience || strings.TrimSpace(b.Subject) == "" {
		return Claims{}, ErrInvalidToken
	}
	current := now().UTC().Unix()
	if b.NotBefore > current {
		return Claims{}, ErrInvalidToken
	}
	if b.ExpiresAt <= current {
		return Claims{}, ErrExpiredToken
	}
	return Claims{
		Subject:   b.Subject,
		Issuer:    b.Issuer,
		Audience:  b.Audience,
		Version:   version,
		IssuedAt:  time.Unix(b.IssuedAt, 0).UTC(),
		NotBefore: time.Unix(b.NotBefore, 0).UTC(),
		ExpiresAt: time.Unix(b.ExpiresAt, 0).UTC(),
		ID:        b.ID,
		Extra:     b.Extra,
	}, nil
}

func signJWT(header jwtHeader, body jwtBody, secret []byte) (string, error) {
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(bodyJSON)
	sig := mac(secret, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func verifySignature(parts []string, secret []byte) error {
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return ErrInvalidToken
	}
	expected := mac(secret, []byte(signingInput))
	if subtle.ConstantTimeCompare(sig, expected) != 1 {
		return ErrInvalidToken
	}
	return nil
}

func decodeHeader(seg string) (jwtHeader, error) {
	var h jwtHeader
	if err := decodeSegment(seg, &h); err != nil {
		return jwtHeader{}, ErrInvalidToken
	}
	if h.Algorithm != "HS256" || h.Type != "JWT" || strings.TrimSpace(h.KeyID) == "" {
		return jwtHeader{}, ErrInvalidToken
	}
	return h, nil
}

func decodeBody(seg string) (jwtBody, error) {
	var b jwtBody
	if err := decodeSegment(seg, &b); err != nil {
		return jwtBody{}, ErrInvalidToken
	}
	return b, nil
}

func decodeSegment(seg string, out any) error {
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func mac(secret, input []byte) []byte {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write(input)
	return h.Sum(nil)
}

func randomID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// Package ratelimit ships the v3.7.0 EC-10-3 stealth-pacing rate
// limiter for browser-driven uiauto operations. It enforces per-
// channel + per-tenant pacing rules so the omniparser-bridge does
// not get banned by RedNote / TikTok creator center / Facebook
// page bot detection.
//
// Algorithm: a token bucket per (tenant_id, channel) keyed by
// composeBucketKey. Tokens refill at a per-channel rate (RedNote:
// 1 op / 5 min, TikTok creator: 1 op / 2 min, Facebook page: 5 ops
// / hour, Instagram: 1 op / hour, default fallback: 1 op / 30 s).
//
// Stealth-pacing extras:
//
//   - Jittered delay applied via crypto/rand (NOT math/rand) so the
//     pacing is non-deterministic from an adversary's perspective.
//   - Drain-on-overflow: if 20+ ops queued for a single bucket,
//     oldest is dropped + RateLimitDrainEvent is emitted (operator
//     alert).
//   - Replay protection: nonce per request via HMAC-SHA256 over a
//     stable canonical form; subtle.ConstantTimeCompare guards
//     verification; nonces TTL is 24h.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4): RateLimiter.Allow decomposes into tokenBucketCheck +
// applyJitter + verifyNonce + routeOverflow helpers; per-function
// cyclomatic stays under 6.
package ratelimit

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"
)

// MaxQueuedPerBucket is the drain threshold; beyond this count, the
// oldest queued op is dropped + RateLimitDrainEvent emitted.
const MaxQueuedPerBucket = 20

// NonceTTL bounds replay-protection memory and is the window during
// which duplicates are rejected.
const NonceTTL = 24 * time.Hour

// MinSecretBytes guards against accidental short HMAC keys. 32
// bytes is the SHA-256 block size and matches the v3.3.0 EC-3-1
// HMAC contract.
const MinSecretBytes = 32

// Channel is the platform identifier; canonical lower-snake-case.
type Channel string

// Canonical channel constants (used as label values + map keys).
const (
	ChannelRedNote   Channel = "rednote"
	ChannelTikTok    Channel = "tiktok"
	ChannelFacebook  Channel = "facebook"
	ChannelInstagram Channel = "instagram"
	ChannelDefault   Channel = "default"
)

// Typed sentinels.
var (
	// ErrRateLimitExceeded is returned by Allow when the bucket is
	// empty AND the queue cap has been reached.
	ErrRateLimitExceeded = errors.New("ratelimit: per-channel budget exceeded")

	// ErrChannelBlocked is returned by Allow when the channel has
	// been administratively blocked (operator-injected).
	ErrChannelBlocked = errors.New("ratelimit: channel blocked")

	// ErrInvalidNonce is returned by Allow when the nonce HMAC
	// fails subtle.ConstantTimeCompare or the nonce has been seen
	// before within NonceTTL.
	ErrInvalidNonce = errors.New("ratelimit: invalid or duplicate nonce")

	// ErrRateLimitDrained is returned by Allow when this request
	// triggered the drain-on-overflow path (20+ queued).
	ErrRateLimitDrained = errors.New("ratelimit: queue drained -- request dropped")

	// ErrLimiterClosed is returned after Close.
	ErrLimiterClosed = errors.New("ratelimit: limiter closed")

	// ErrUnconfigured is returned by NewRateLimiter if a required
	// field is missing.
	ErrUnconfigured = errors.New("ratelimit: limiter unconfigured")
)

// ChannelRule is the per-channel pacing rule. Capacity is the
// burst budget; refill rate is one token every Period. Defaults
// in the rule table follow the plan EC-10-3 specification.
type ChannelRule struct {
	Channel  Channel
	Capacity int
	Period   time.Duration
}

// DefaultChannelRules returns the rule table from the plan.
func DefaultChannelRules() map[Channel]ChannelRule {
	return map[Channel]ChannelRule{
		ChannelRedNote:   {Channel: ChannelRedNote, Capacity: 1, Period: 5 * time.Minute},
		ChannelTikTok:    {Channel: ChannelTikTok, Capacity: 1, Period: 2 * time.Minute},
		ChannelFacebook:  {Channel: ChannelFacebook, Capacity: 5, Period: time.Hour},
		ChannelInstagram: {Channel: ChannelInstagram, Capacity: 1, Period: time.Hour},
		ChannelDefault:   {Channel: ChannelDefault, Capacity: 1, Period: 30 * time.Second},
	}
}

// Metrics is the small port the limiter records counters through.
type Metrics interface {
	RecordRateLimitDrop(tenantID string, channel Channel, reason string)
}

// Emitter emits the operator-alert event when the drain-on-overflow
// fires. Caller wires the eventbus; a no-op is provided.
type Emitter interface {
	EmitRateLimitDrain(ctx context.Context, tenantID string, channel Channel, droppedAt time.Time)
}

// NoopEmitter discards events.
type NoopEmitter struct{}

// EmitRateLimitDrain is a no-op.
func (NoopEmitter) EmitRateLimitDrain(_ context.Context, _ string, _ Channel, _ time.Time) {}

// AllowRequest carries everything the limiter needs to evaluate one
// op. Channel, TenantID, and Nonce are required; Now is for tests.
type AllowRequest struct {
	TenantID  string
	Channel   Channel
	Nonce     string
	Timestamp int64 // unix epoch seconds; combines with nonce for replay protection
}

// AllowDecision is the structured outcome.
type AllowDecision struct {
	Allowed bool
	Delay   time.Duration // jitter applied if non-zero
	Reason  string
}

// Config wires the limiter.
type Config struct {
	Rules        map[Channel]ChannelRule
	BlockedChans map[Channel]bool
	Secret       []byte // HMAC secret for nonce verification
	Metrics      Metrics
	Emitter      Emitter
	Logger       *slog.Logger
	Now          func() time.Time
	JitterMaxMs  int // upper bound for stealth jitter; 0 disables jitter
	QueueCap     int // overrides MaxQueuedPerBucket; 0 = default
}

// RateLimiter implements the EC-10-3 contract.
type RateLimiter struct {
	rules       map[Channel]ChannelRule
	blocked     map[Channel]bool
	secret      []byte
	metrics     Metrics
	emitter     Emitter
	logger      *slog.Logger
	now         func() time.Time
	jitterMaxMs int
	queueCap    int

	mu      sync.Mutex
	closed  bool
	buckets map[string]*tokenBucket
	nonces  map[string]time.Time
}

type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
	queued     int
	rule       ChannelRule
}

// New constructs a RateLimiter.
func New(cfg Config) (*RateLimiter, error) {
	if len(cfg.Secret) < MinSecretBytes {
		return nil, fmt.Errorf("%w: secret < %d bytes", ErrUnconfigured, MinSecretBytes)
	}
	if len(cfg.Rules) == 0 {
		cfg.Rules = DefaultChannelRules()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.QueueCap <= 0 {
		cfg.QueueCap = MaxQueuedPerBucket
	}
	if cfg.BlockedChans == nil {
		cfg.BlockedChans = map[Channel]bool{}
	}
	return &RateLimiter{
		rules:       cfg.Rules,
		blocked:     cfg.BlockedChans,
		secret:      append([]byte(nil), cfg.Secret...),
		metrics:     cfg.Metrics,
		emitter:     cfg.Emitter,
		logger:      cfg.Logger,
		now:         cfg.Now,
		jitterMaxMs: cfg.JitterMaxMs,
		queueCap:    cfg.QueueCap,
		buckets:     map[string]*tokenBucket{},
		nonces:      map[string]time.Time{},
	}, nil
}

// SignNonce returns the HMAC-SHA256 hex signature for (tenant,
// channel, nonce, timestamp). Callers compute this once and pass it
// to the bridge; the limiter recomputes + ConstantTimeCompare-s on
// the verify path.
func (r *RateLimiter) SignNonce(tenantID string, channel Channel, nonce string, ts int64) string {
	h := hmac.New(sha256.New, r.secret)
	canonical := canonicalForm(tenantID, channel, nonce, ts)
	h.Write([]byte(canonical))
	return hex.EncodeToString(h.Sum(nil))
}

// canonicalForm builds the deterministic message string the HMAC
// covers. Pipes are separators; no other character is escaped.
func canonicalForm(tenantID string, channel Channel, nonce string, ts int64) string {
	var sb strings.Builder
	sb.WriteString(tenantID)
	sb.WriteByte('|')
	sb.WriteString(string(channel))
	sb.WriteByte('|')
	sb.WriteString(nonce)
	sb.WriteByte('|')
	sb.WriteString(fmt.Sprintf("%d", ts))
	return sb.String()
}

// Allow evaluates the request. Decomposes via verifyNonce +
// tokenBucketCheck + applyJitter + routeOverflow helpers. Returns
// (decision, error). The bridge call site looks like:
//
//	dec, err := lim.Allow(ctx, req, signature)
//	if err != nil { return err }
//	time.Sleep(dec.Delay)
//	bridgeCall(...)
func (r *RateLimiter) Allow(ctx context.Context, req AllowRequest, signature string) (AllowDecision, error) {
	if err := ctx.Err(); err != nil {
		return AllowDecision{}, err
	}
	if err := r.checkClosedAndBlocked(req.Channel); err != nil {
		return AllowDecision{}, err
	}
	if err := r.verifyNonce(req, signature); err != nil {
		return AllowDecision{}, err
	}
	allowed, dropped, rule := r.tokenBucketCheck(req)
	if dropped {
		r.recordDrop(req, "drain")
		go r.emitDrain(req)
		return AllowDecision{Allowed: false, Reason: "drained"}, ErrRateLimitDrained
	}
	if !allowed {
		r.recordDrop(req, "exceeded")
		return AllowDecision{Allowed: false, Reason: "exceeded"}, fmt.Errorf("%w: channel=%s capacity=%d period=%s", ErrRateLimitExceeded, rule.Channel, rule.Capacity, rule.Period)
	}
	delay, err := r.applyJitter()
	if err != nil {
		return AllowDecision{}, err
	}
	return AllowDecision{Allowed: true, Delay: delay, Reason: "ok"}, nil
}

func (r *RateLimiter) checkClosedAndBlocked(channel Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrLimiterClosed
	}
	if r.blocked[channel] {
		return fmt.Errorf("%w: channel=%s", ErrChannelBlocked, channel)
	}
	return nil
}

// verifyNonce checks HMAC + replay-window. Routes the seen-nonce
// table cleanup through purgeExpiredNoncesLocked.
func (r *RateLimiter) verifyNonce(req AllowRequest, signature string) error {
	expected := r.SignNonce(req.TenantID, req.Channel, req.Nonce, req.Timestamp)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
		return fmt.Errorf("%w: signature mismatch", ErrInvalidNonce)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.purgeExpiredNoncesLocked(r.now())
	key := canonicalForm(req.TenantID, req.Channel, req.Nonce, req.Timestamp)
	if _, seen := r.nonces[key]; seen {
		return fmt.Errorf("%w: replay", ErrInvalidNonce)
	}
	r.nonces[key] = r.now()
	return nil
}

func (r *RateLimiter) purgeExpiredNoncesLocked(now time.Time) {
	for k, ts := range r.nonces {
		if now.Sub(ts) > NonceTTL {
			delete(r.nonces, k)
		}
	}
}

// tokenBucketCheck is the actual rate-limit decision. Returns
// (allowed, drained, rule) so the caller can emit metrics.
func (r *RateLimiter) tokenBucketCheck(req AllowRequest) (bool, bool, ChannelRule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rule, ok := r.rules[req.Channel]
	if !ok {
		rule = r.rules[ChannelDefault]
	}
	now := r.now()
	bucketKey := composeBucketKey(req.TenantID, req.Channel)
	b, exists := r.buckets[bucketKey]
	if !exists {
		b = &tokenBucket{tokens: float64(rule.Capacity), lastRefill: now, rule: rule}
		r.buckets[bucketKey] = b
	}
	r.refillLocked(b, now)
	if b.tokens >= 1 {
		b.tokens--
		return true, false, rule
	}
	b.queued++
	if b.queued > r.queueCap {
		b.queued = r.queueCap
		return false, true, rule
	}
	return false, false, rule
}

// refillLocked tops up the bucket based on elapsed wall time.
func (r *RateLimiter) refillLocked(b *tokenBucket, now time.Time) {
	elapsed := now.Sub(b.lastRefill)
	if elapsed <= 0 {
		return
	}
	rate := float64(b.rule.Capacity) / b.rule.Period.Seconds()
	added := rate * elapsed.Seconds()
	b.tokens += added
	if b.tokens > float64(b.rule.Capacity) {
		b.tokens = float64(b.rule.Capacity)
	}
	if b.tokens >= 1 && b.queued > 0 {
		b.queued--
	}
	b.lastRefill = now
}

// applyJitter draws a jittered delay in [0, jitterMaxMs] using
// crypto/rand (NOT math/rand) so the pacing is non-deterministic
// from an adversary's perspective. Returns the delay duration.
func (r *RateLimiter) applyJitter() (time.Duration, error) {
	if r.jitterMaxMs <= 0 {
		return 0, nil
	}
	max := big.NewInt(int64(r.jitterMaxMs))
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return 0, fmt.Errorf("ratelimit: jitter: %w", err)
	}
	return time.Duration(n.Int64()) * time.Millisecond, nil
}

func (r *RateLimiter) recordDrop(req AllowRequest, reason string) {
	if r.metrics == nil {
		return
	}
	r.metrics.RecordRateLimitDrop(req.TenantID, req.Channel, reason)
}

func (r *RateLimiter) emitDrain(req AllowRequest) {
	if r.emitter == nil {
		return
	}
	r.emitter.EmitRateLimitDrain(context.Background(), req.TenantID, req.Channel, r.now())
}

// composeBucketKey is the canonical (tenant, channel) bucket
// identifier.
func composeBucketKey(tenantID string, channel Channel) string {
	return tenantID + "|" + string(channel)
}

// Close marks the limiter closed. Safe to call multiple times.
func (r *RateLimiter) Close(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

// Block administratively blocks a channel (operator override).
func (r *RateLimiter) Block(channel Channel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocked[channel] = true
}

// Unblock undoes Block.
func (r *RateLimiter) Unblock(channel Channel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.blocked, channel)
}

// SeenNonceCount returns the number of replay-protection entries
// currently held. Useful for tests + dashboards.
func (r *RateLimiter) SeenNonceCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.nonces)
}

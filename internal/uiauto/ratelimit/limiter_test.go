package ratelimit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type recMetrics struct {
	mu    sync.Mutex
	drops []string
}

func (m *recMetrics) RecordRateLimitDrop(tenantID string, channel Channel, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drops = append(m.drops, tenantID+"|"+string(channel)+"|"+reason)
}

func (m *recMetrics) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.drops)
}

type recEmitter struct {
	mu    sync.Mutex
	calls []string
}

func (e *recEmitter) EmitRateLimitDrain(_ context.Context, tenantID string, channel Channel, _ time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, tenantID+"|"+string(channel))
}

func (e *recEmitter) Count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.calls)
}

func newSecret(t *testing.T) []byte {
	t.Helper()
	out := make([]byte, MinSecretBytes)
	for i := range out {
		out[i] = byte(0xAB ^ i)
	}
	return out
}

func newLimiter(t *testing.T, m Metrics, e Emitter, now func() time.Time, jitter int) *RateLimiter {
	t.Helper()
	cfg := Config{
		Secret:      newSecret(t),
		Metrics:     m,
		Emitter:     e,
		Now:         now,
		JitterMaxMs: jitter,
		QueueCap:    5, // smaller for faster overflow testing
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return r
}

func signed(r *RateLimiter, tenant string, ch Channel, nonce string, ts int64) (AllowRequest, string) {
	req := AllowRequest{TenantID: tenant, Channel: ch, Nonce: nonce, Timestamp: ts}
	return req, r.SignNonce(tenant, ch, nonce, ts)
}

func TestRateLimiter_AllowsWithinBudget(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	r := newLimiter(t, nil, nil, func() time.Time { return now }, 0)
	defer r.Close(context.Background())
	req, sig := signed(r, "tenant-a", ChannelTikTok, "n-1", now.Unix())
	dec, err := r.Allow(context.Background(), req, sig)
	if err != nil {
		t.Fatalf("first allow: %v", err)
	}
	if !dec.Allowed {
		t.Fatalf("first allow returned Allowed=false")
	}
}

func TestRateLimiter_BlocksWhenExceeded(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	clock := struct {
		mu sync.Mutex
		t  time.Time
	}{t: now}
	nowFn := func() time.Time {
		clock.mu.Lock()
		defer clock.mu.Unlock()
		return clock.t
	}
	m := &recMetrics{}
	r := newLimiter(t, m, nil, nowFn, 0)
	defer r.Close(context.Background())
	// TikTok: 1 / 2min. Two back-to-back ops -> second should be exceeded.
	req, sig := signed(r, "tenant-a", ChannelTikTok, "n-1", now.Unix())
	if _, err := r.Allow(context.Background(), req, sig); err != nil {
		t.Fatalf("first: %v", err)
	}
	req2, sig2 := signed(r, "tenant-a", ChannelTikTok, "n-2", now.Unix()+1)
	_, err := r.Allow(context.Background(), req2, sig2)
	if !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatalf("want ErrRateLimitExceeded, got %v", err)
	}
	if m.Count() == 0 {
		t.Fatalf("want metric drop; got 0")
	}
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	t.Parallel()
	clock := struct {
		mu sync.Mutex
		t  time.Time
	}{t: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)}
	advance := func(d time.Duration) {
		clock.mu.Lock()
		clock.t = clock.t.Add(d)
		clock.mu.Unlock()
	}
	nowFn := func() time.Time {
		clock.mu.Lock()
		defer clock.mu.Unlock()
		return clock.t
	}
	r := newLimiter(t, nil, nil, nowFn, 0)
	defer r.Close(context.Background())
	req, sig := signed(r, "tenant-a", ChannelTikTok, "n-1", nowFn().Unix())
	if _, err := r.Allow(context.Background(), req, sig); err != nil {
		t.Fatalf("first: %v", err)
	}
	advance(2 * time.Minute)
	req2, sig2 := signed(r, "tenant-a", ChannelTikTok, "n-2", nowFn().Unix())
	if _, err := r.Allow(context.Background(), req2, sig2); err != nil {
		t.Fatalf("after refill: %v", err)
	}
}

func TestRateLimiter_JitterAddedToDelay(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	r := newLimiter(t, nil, nil, func() time.Time { return now }, 100)
	defer r.Close(context.Background())
	seen := map[time.Duration]int{}
	for i := 0; i < 100; i++ {
		// Use Facebook (5 / hr) so the bucket isn't an immediate
		// blocker.
		req, sig := signed(r, "tenant-fb", ChannelFacebook, fmtNonce(i), now.Unix()+int64(i))
		dec, err := r.Allow(context.Background(), req, sig)
		if err != nil {
			break
		}
		if dec.Delay > 0 {
			seen[dec.Delay]++
		}
	}
	if len(seen) < 2 {
		t.Fatalf("jitter should produce >1 distinct delays in 100 calls; got %v", seen)
	}
}

func TestRateLimiter_DrainOldestOnOverflow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	m := &recMetrics{}
	e := &recEmitter{}
	r := newLimiter(t, m, e, func() time.Time { return now }, 0)
	defer r.Close(context.Background())
	// RedNote: 1 / 5min. Burn token, then queue past cap to trigger
	// drain.
	first, firstSig := signed(r, "tenant-x", ChannelRedNote, "n-burn", now.Unix())
	if _, err := r.Allow(context.Background(), first, firstSig); err != nil {
		t.Fatalf("burn: %v", err)
	}
	// 5 attempts to fill queue; 6th drains.
	for i := 0; i < 5; i++ {
		req, sig := signed(r, "tenant-x", ChannelRedNote, fmtNonce(i), now.Unix()+int64(i+1))
		_, err := r.Allow(context.Background(), req, sig)
		if !errors.Is(err, ErrRateLimitExceeded) {
			t.Fatalf("queue fill #%d: got %v", i, err)
		}
	}
	overflow, oSig := signed(r, "tenant-x", ChannelRedNote, "n-drain", now.Unix()+99)
	_, err := r.Allow(context.Background(), overflow, oSig)
	if !errors.Is(err, ErrRateLimitDrained) {
		t.Fatalf("want ErrRateLimitDrained, got %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if e.Count() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e.Count() == 0 {
		t.Fatalf("emitter should fire on drain")
	}
}

func TestRateLimiter_ReplayProtectionRejectsDuplicate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	r := newLimiter(t, nil, nil, func() time.Time { return now }, 0)
	defer r.Close(context.Background())
	req, sig := signed(r, "tenant-a", ChannelFacebook, "n-1", now.Unix())
	if _, err := r.Allow(context.Background(), req, sig); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := r.Allow(context.Background(), req, sig); !errors.Is(err, ErrInvalidNonce) {
		t.Fatalf("want ErrInvalidNonce on replay, got %v", err)
	}
}

func TestRateLimiter_BadSignatureRejected(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	r := newLimiter(t, nil, nil, func() time.Time { return now }, 0)
	defer r.Close(context.Background())
	req := AllowRequest{TenantID: "tenant-a", Channel: ChannelFacebook, Nonce: "n-1", Timestamp: now.Unix()}
	if _, err := r.Allow(context.Background(), req, "deadbeef"); !errors.Is(err, ErrInvalidNonce) {
		t.Fatalf("want ErrInvalidNonce on bad sig, got %v", err)
	}
}

func TestRateLimiter_PerChannelIsolation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	r := newLimiter(t, nil, nil, func() time.Time { return now }, 0)
	defer r.Close(context.Background())
	for _, ch := range []Channel{ChannelTikTok, ChannelRedNote} {
		req, sig := signed(r, "tenant-a", ch, "burn-"+string(ch), now.Unix())
		if _, err := r.Allow(context.Background(), req, sig); err != nil {
			t.Fatalf("burn %s: %v", ch, err)
		}
	}
	// RedNote second call should fail; TikTok's bucket is independent.
	req2, sig2 := signed(r, "tenant-a", ChannelRedNote, "second", now.Unix()+1)
	if _, err := r.Allow(context.Background(), req2, sig2); !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatalf("rednote second: want exceeded, got %v", err)
	}
}

func TestRateLimiter_BlockedChannel(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	r := newLimiter(t, nil, nil, func() time.Time { return now }, 0)
	defer r.Close(context.Background())
	r.Block(ChannelTikTok)
	req, sig := signed(r, "tenant-a", ChannelTikTok, "n", now.Unix())
	if _, err := r.Allow(context.Background(), req, sig); !errors.Is(err, ErrChannelBlocked) {
		t.Fatalf("want ErrChannelBlocked, got %v", err)
	}
	r.Unblock(ChannelTikTok)
	if _, err := r.Allow(context.Background(), req, sig); err != nil {
		t.Fatalf("after unblock: %v", err)
	}
}

func TestRateLimiter_NonceTTLPurge(t *testing.T) {
	t.Parallel()
	clock := struct {
		mu sync.Mutex
		t  time.Time
	}{t: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)}
	advance := func(d time.Duration) {
		clock.mu.Lock()
		clock.t = clock.t.Add(d)
		clock.mu.Unlock()
	}
	nowFn := func() time.Time {
		clock.mu.Lock()
		defer clock.mu.Unlock()
		return clock.t
	}
	r := newLimiter(t, nil, nil, nowFn, 0)
	defer r.Close(context.Background())
	req, sig := signed(r, "tenant-a", ChannelFacebook, "n-1", nowFn().Unix())
	if _, err := r.Allow(context.Background(), req, sig); err != nil {
		t.Fatalf("first: %v", err)
	}
	if r.SeenNonceCount() != 1 {
		t.Fatalf("want 1 seen nonce, got %d", r.SeenNonceCount())
	}
	advance(NonceTTL + time.Hour)
	// Burn capacity then trigger purge via second-call attempt.
	req2, sig2 := signed(r, "tenant-a", ChannelFacebook, "n-2", nowFn().Unix())
	if _, err := r.Allow(context.Background(), req2, sig2); err != nil {
		t.Fatalf("after purge: %v", err)
	}
	if c := r.SeenNonceCount(); c != 1 {
		t.Fatalf("want 1 (after purge); got %d", c)
	}
}

func TestRateLimiter_RejectsShortSecret(t *testing.T) {
	t.Parallel()
	_, err := New(Config{Secret: []byte("short")})
	if !errors.Is(err, ErrUnconfigured) {
		t.Fatalf("want ErrUnconfigured, got %v", err)
	}
}

func TestRateLimiter_DefaultRulesPopulated(t *testing.T) {
	t.Parallel()
	rules := DefaultChannelRules()
	for _, ch := range []Channel{ChannelRedNote, ChannelTikTok, ChannelFacebook, ChannelInstagram, ChannelDefault} {
		if _, ok := rules[ch]; !ok {
			t.Fatalf("default rules missing channel %s", ch)
		}
	}
}

func TestRateLimiter_ClosedRefusesNew(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	r := newLimiter(t, nil, nil, func() time.Time { return now }, 0)
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	req, sig := signed(r, "tenant-a", ChannelFacebook, "n", now.Unix())
	if _, err := r.Allow(context.Background(), req, sig); !errors.Is(err, ErrLimiterClosed) {
		t.Fatalf("want ErrLimiterClosed, got %v", err)
	}
}

// fmtNonce returns a deterministic nonce for the i-th attempt.
func fmtNonce(i int) string {
	var sb strings.Builder
	sb.WriteString("nonce-")
	sb.WriteString(string(rune('a' + i%26)))
	sb.WriteString(string(rune('0' + i/26)))
	return sb.String()
}

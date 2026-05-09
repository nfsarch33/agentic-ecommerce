package social

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeExchanger struct {
	mu             sync.Mutex
	exchangeCalls  int
	refreshCalls   int
	exchangeResult TikTokToken
	refreshResult  TikTokToken
	exchangeErr    error
	refreshErr     error
}

func (f *fakeExchanger) Exchange(_ context.Context, req OAuthBootstrapRequest) (TikTokToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exchangeCalls++
	if f.exchangeErr != nil {
		return TikTokToken{}, f.exchangeErr
	}
	out := f.exchangeResult
	out.TenantID = req.TenantID
	out.ShopID = req.ShopID
	return out, nil
}

func (f *fakeExchanger) Refresh(_ context.Context, _ string, tenantID string) (TikTokToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshCalls++
	if f.refreshErr != nil {
		return TikTokToken{}, f.refreshErr
	}
	out := f.refreshResult
	out.TenantID = tenantID
	return out, nil
}

func TestNewPKCEPair_ProducesS256Challenge(t *testing.T) {
	t.Parallel()
	pair, err := NewPKCEPair()
	if err != nil {
		t.Fatalf("NewPKCEPair: %v", err)
	}
	if len(pair.Verifier) < 43 {
		t.Fatalf("verifier too short: %d", len(pair.Verifier))
	}
	if len(pair.Challenge) < 43 {
		t.Fatalf("challenge too short: %d", len(pair.Challenge))
	}
	if pair.Verifier == pair.Challenge {
		t.Fatalf("verifier and challenge must differ")
	}
}

func TestMemoryTokenStore_GetPutRoundTrip(t *testing.T) {
	t.Parallel()
	store := NewMemoryTokenStore()
	ctx := context.Background()
	if _, err := store.Get(ctx, "tenant-a"); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
	tok := TikTokToken{TenantID: "tenant-a", AccessToken: "abc", RefreshToken: "def", ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.Put(ctx, tok); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Get(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccessToken != "abc" {
		t.Fatalf("got %s", got.AccessToken)
	}
}

func TestMemoryTokenStore_PutRequiresTenantID(t *testing.T) {
	t.Parallel()
	store := NewMemoryTokenStore()
	if err := store.Put(context.Background(), TikTokToken{}); !errors.Is(err, ErrTikTokUnconfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenManager_BootstrapPersistsToken(t *testing.T) {
	t.Parallel()
	store := NewMemoryTokenStore()
	exchanger := &fakeExchanger{exchangeResult: TikTokToken{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	}}
	mgr, err := NewTokenManager(TokenManagerConfig{Store: store, Exchanger: exchanger})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	tok, err := mgr.Bootstrap(context.Background(), OAuthBootstrapRequest{
		TenantID:          "tenant-1",
		ShopID:            "shop-1",
		AuthorizationCode: "code-xyz",
		Verifier:          "ver-abc",
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if tok.AccessToken != "access-1" {
		t.Fatalf("got %s", tok.AccessToken)
	}
	stored, err := store.Get(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.AccessToken != "access-1" || stored.TenantID != "tenant-1" {
		t.Fatalf("stored = %+v", stored)
	}
	if exchanger.exchangeCalls != 1 {
		t.Fatalf("exchangeCalls = %d", exchanger.exchangeCalls)
	}
}

func TestTokenManager_RefreshOnExpiry(t *testing.T) {
	t.Parallel()
	store := NewMemoryTokenStore()
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	expired := TikTokToken{TenantID: "tenant-1", AccessToken: "old", RefreshToken: "rt", ExpiresAt: now.Add(-time.Minute)}
	if err := store.Put(context.Background(), expired); err != nil {
		t.Fatalf("Put: %v", err)
	}
	exchanger := &fakeExchanger{refreshResult: TikTokToken{
		AccessToken:  "new",
		RefreshToken: "rt-2",
		ExpiresAt:    now.Add(time.Hour),
	}}
	mgr, err := NewTokenManager(TokenManagerConfig{
		Store:     store,
		Exchanger: exchanger,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	tok, err := mgr.AccessToken(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok.AccessToken != "new" {
		t.Fatalf("got %s", tok.AccessToken)
	}
	if exchanger.refreshCalls != 1 {
		t.Fatalf("refreshCalls = %d", exchanger.refreshCalls)
	}
}

func TestTokenManager_RefreshSingleFlightPerTenant(t *testing.T) {
	t.Parallel()
	store := NewMemoryTokenStore()
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	expired := TikTokToken{TenantID: "tenant-1", AccessToken: "old", RefreshToken: "rt", ExpiresAt: now.Add(-time.Minute)}
	if err := store.Put(context.Background(), expired); err != nil {
		t.Fatalf("Put: %v", err)
	}
	exchanger := &fakeExchanger{refreshResult: TikTokToken{AccessToken: "new", ExpiresAt: now.Add(time.Hour)}}
	mgr, err := NewTokenManager(TokenManagerConfig{
		Store:     store,
		Exchanger: exchanger,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	const N = 8
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			if _, err := mgr.AccessToken(context.Background(), "tenant-1"); err != nil {
				t.Errorf("AccessToken: %v", err)
			}
		}()
	}
	wg.Wait()
	if exchanger.refreshCalls != 1 {
		t.Fatalf("refreshCalls = %d, want 1 (single-flight)", exchanger.refreshCalls)
	}
}

func TestTokenManager_BootstrapValidatesInput(t *testing.T) {
	t.Parallel()
	mgr, err := NewTokenManager(TokenManagerConfig{Store: NewMemoryTokenStore(), Exchanger: &fakeExchanger{}})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	cases := map[string]OAuthBootstrapRequest{
		"missing tenant":   {AuthorizationCode: "abc", Verifier: "ver"},
		"missing code":     {TenantID: "t", Verifier: "ver"},
		"missing verifier": {TenantID: "t", AuthorizationCode: "abc"},
	}
	for name, req := range cases {
		name, req := name, req
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := mgr.Bootstrap(context.Background(), req)
			if !errors.Is(err, ErrTikTokUnconfigured) {
				t.Fatalf("err = %v, want ErrTikTokUnconfigured", err)
			}
		})
	}
}

func TestNewTokenManager_RequiresStoreAndExchanger(t *testing.T) {
	t.Parallel()
	if _, err := NewTokenManager(TokenManagerConfig{Exchanger: &fakeExchanger{}}); !errors.Is(err, ErrTikTokUnconfigured) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewTokenManager(TokenManagerConfig{Store: NewMemoryTokenStore()}); !errors.Is(err, ErrTikTokUnconfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestTikTokToken_IsExpired(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		tok  TikTokToken
		want bool
	}{
		{"zero exp", TikTokToken{}, true},
		{"future", TikTokToken{ExpiresAt: now.Add(2 * time.Minute)}, false},
		{"within skew", TikTokToken{ExpiresAt: now.Add(30 * time.Second)}, true},
		{"past", TikTokToken{ExpiresAt: now.Add(-time.Minute)}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.tok.IsExpired(now, time.Minute); got != tc.want {
				t.Fatalf("IsExpired = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOAuthBootstrapRequest_Validate(t *testing.T) {
	t.Parallel()
	good := OAuthBootstrapRequest{TenantID: "t", AuthorizationCode: "c", Verifier: "v"}
	if err := good.Validate(); err != nil {
		t.Fatalf("Validate good: %v", err)
	}
	bad := OAuthBootstrapRequest{TenantID: "  "}
	err := bad.Validate()
	if !errors.Is(err, ErrTikTokUnconfigured) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "TenantID required") {
		t.Fatalf("err message = %v", err)
	}
}

package social

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeFacebookExchanger is the in-test FacebookTokenExchanger.
type fakeFacebookExchanger struct {
	calls atomic.Int32
	tok   FacebookToken
	err   error
}

func (f *fakeFacebookExchanger) Exchange(_ context.Context, req FacebookOAuthBootstrapRequest) (FacebookToken, error) {
	f.calls.Add(1)
	if f.err != nil {
		return FacebookToken{}, f.err
	}
	tok := f.tok
	if tok.TenantID == "" {
		tok.TenantID = req.TenantID
	}
	if tok.PageID == "" {
		tok.PageID = req.PageID
	}
	return tok, nil
}

func TestFacebookTokenManager_Bootstrap_PersistsLongLivedToken(t *testing.T) {
	t.Parallel()
	store := NewFacebookMemoryTokenStore()
	exchanger := &fakeFacebookExchanger{
		tok: FacebookToken{
			AccessToken: "long-lived-page-token",
			ExpiresAt:   time.Now().Add(60 * 24 * time.Hour),
			Scopes:      []string{"catalog_management", "commerce_account_read_orders"},
		},
	}
	mgr, err := NewFacebookTokenManager(FacebookTokenManagerConfig{Store: store, Exchanger: exchanger})
	if err != nil {
		t.Fatalf("NewFacebookTokenManager: %v", err)
	}
	tok, err := mgr.Bootstrap(context.Background(), FacebookOAuthBootstrapRequest{
		TenantID:        "tenant-1",
		PageID:          "page-1",
		ShortLivedToken: "short-lived-token",
		Scopes:          []string{"catalog_management"},
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if tok.AccessToken != "long-lived-page-token" || tok.TenantID != "tenant-1" || tok.PageID != "page-1" {
		t.Fatalf("token = %+v", tok)
	}
	if exchanger.calls.Load() != 1 {
		t.Fatalf("exchanger calls = %d", exchanger.calls.Load())
	}
	got, err := store.Get(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got.AccessToken != tok.AccessToken {
		t.Fatalf("store mismatch: %+v", got)
	}
}

func TestFacebookTokenManager_Bootstrap_RejectsInvalid(t *testing.T) {
	t.Parallel()
	mgr, err := NewFacebookTokenManager(FacebookTokenManagerConfig{Store: NewFacebookMemoryTokenStore(), Exchanger: &fakeFacebookExchanger{}})
	if err != nil {
		t.Fatalf("NewFacebookTokenManager: %v", err)
	}
	cases := map[string]FacebookOAuthBootstrapRequest{
		"missing tenant": {PageID: "p", ShortLivedToken: "t"},
		"missing page":   {TenantID: "t", ShortLivedToken: "t"},
		"missing token":  {TenantID: "t", PageID: "p"},
	}
	for name, req := range cases {
		name, req := name, req
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := mgr.Bootstrap(context.Background(), req)
			if !errors.Is(err, ErrFacebookUnconfigured) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFacebookTokenManager_AccessToken_RejectsExpired(t *testing.T) {
	t.Parallel()
	store := NewFacebookMemoryTokenStore()
	_ = store.Put(context.Background(), FacebookToken{
		TenantID:    "tenant-1",
		AccessToken: "expired",
		ExpiresAt:   time.Now().Add(-time.Hour),
	})
	mgr, err := NewFacebookTokenManager(FacebookTokenManagerConfig{Store: store, Exchanger: &fakeFacebookExchanger{}})
	if err != nil {
		t.Fatalf("NewFacebookTokenManager: %v", err)
	}
	_, err = mgr.AccessToken(context.Background(), "tenant-1")
	if !errors.Is(err, ErrFacebookAuthFailed) {
		t.Fatalf("err = %v, want ErrFacebookAuthFailed", err)
	}
}

func TestFacebookTokenManager_AccessToken_NotFound(t *testing.T) {
	t.Parallel()
	mgr, err := NewFacebookTokenManager(FacebookTokenManagerConfig{Store: NewFacebookMemoryTokenStore(), Exchanger: &fakeFacebookExchanger{}})
	if err != nil {
		t.Fatalf("NewFacebookTokenManager: %v", err)
	}
	_, err = mgr.AccessToken(context.Background(), "missing")
	if !errors.Is(err, ErrFacebookTokenNotFound) {
		t.Fatalf("err = %v, want ErrFacebookTokenNotFound", err)
	}
}

func TestFacebookTokenManager_AccessToken_FreshSucceeds(t *testing.T) {
	t.Parallel()
	store := NewFacebookMemoryTokenStore()
	_ = store.Put(context.Background(), FacebookToken{
		TenantID:    "tenant-1",
		AccessToken: "fresh",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	mgr, err := NewFacebookTokenManager(FacebookTokenManagerConfig{Store: store, Exchanger: &fakeFacebookExchanger{}})
	if err != nil {
		t.Fatalf("NewFacebookTokenManager: %v", err)
	}
	tok, err := mgr.AccessToken(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok.AccessToken != "fresh" {
		t.Fatalf("token = %+v", tok)
	}
}

func TestNewFacebookTokenManager_RejectsUnconfigured(t *testing.T) {
	t.Parallel()
	cases := map[string]FacebookTokenManagerConfig{
		"missing store":     {Exchanger: &fakeFacebookExchanger{}},
		"missing exchanger": {Store: NewFacebookMemoryTokenStore()},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewFacebookTokenManager(cfg)
			if !errors.Is(err, ErrFacebookUnconfigured) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFacebookMemoryTokenStore_PutRequiresTenantID(t *testing.T) {
	t.Parallel()
	err := NewFacebookMemoryTokenStore().Put(context.Background(), FacebookToken{})
	if !errors.Is(err, ErrFacebookUnconfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestFacebookToken_IsExpiredZero(t *testing.T) {
	t.Parallel()
	if !(FacebookToken{}.IsExpired(time.Now(), time.Second)) {
		t.Fatal("zero-expiry token must be expired")
	}
}

func TestFacebookTokenManager_TenantLockReusesEntry(t *testing.T) {
	t.Parallel()
	mgr, err := NewFacebookTokenManager(FacebookTokenManagerConfig{Store: NewFacebookMemoryTokenStore(), Exchanger: &fakeFacebookExchanger{}})
	if err != nil {
		t.Fatalf("NewFacebookTokenManager: %v", err)
	}
	a := mgr.tenantLock("tenant-x")
	b := mgr.tenantLock("tenant-x")
	if a != b {
		t.Fatalf("tenantLock did not reuse entry")
	}
}

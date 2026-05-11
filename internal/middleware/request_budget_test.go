package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestBudget_DisabledWhenDefaultZero(t *testing.T) {
	t.Parallel()
	mw := RequestBudget(RequestBudgetConfig{})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, deadlineSet := r.Context().Deadline(); deadlineSet {
			t.Fatalf("deadline must not be set when budget = 0")
		}
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestRequestBudget_AppliesDefault(t *testing.T) {
	t.Parallel()
	mw := RequestBudget(RequestBudgetConfig{Default: 50 * time.Millisecond})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Fatalf("expected deadline to be set")
		}
		if time.Until(deadline) > 50*time.Millisecond {
			t.Fatalf("deadline far in the future: %s", deadline)
		}
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestRequestBudget_TenantOverride(t *testing.T) {
	t.Parallel()
	mw := RequestBudget(RequestBudgetConfig{
		Default: 10 * time.Millisecond,
		TenantOverride: func(r *http.Request) (time.Duration, bool) {
			if r.Header.Get("X-Tenant-ID") == "vip" {
				return time.Hour, true
			}
			return 0, false
		},
	})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Fatalf("expected deadline")
		}
		if time.Until(deadline) < time.Minute {
			t.Fatalf("vip override not applied: %s", deadline)
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Tenant-ID", "vip")
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func TestRequestBudget_CancelsHandlerContextWhenBudgetExpires(t *testing.T) {
	t.Parallel()
	mw := RequestBudget(RequestBudgetConfig{Default: 5 * time.Millisecond})
	done := make(chan error, 1)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			done <- r.Context().Err()
		case <-time.After(time.Second):
			done <- errors.New("budget did not fire")
		}
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ctx err = %v want DeadlineExceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not signal done")
	}
}

package sms_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/sms"
)

// --- RateLimiter ---

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	t.Parallel()
	rl := sms.NewRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if err := rl.Allow("user1"); err != nil {
			t.Fatalf("attempt %d: unexpected error: %v", i, err)
		}
	}
}

func TestRateLimiter_BlocksAtLimit(t *testing.T) {
	t.Parallel()
	rl := sms.NewRateLimiter(2, time.Minute)
	_ = rl.Allow("user2")
	_ = rl.Allow("user2")
	if err := rl.Allow("user2"); !errors.Is(err, sms.ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
}

func TestRateLimiter_SlidingWindowExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	rl := sms.NewRateLimiter(2, time.Minute)
	rl.WithClock(func() time.Time { return now })

	_ = rl.Allow("u3")
	_ = rl.Allow("u3")
	// Advance time past the window.
	now = now.Add(2 * time.Minute)
	rl.WithClock(func() time.Time { return now })
	if err := rl.Allow("u3"); err != nil {
		t.Fatalf("window should have reset, got: %v", err)
	}
}

// --- OptOutStore ---

func TestOptOutStore_OptOut(t *testing.T) {
	t.Parallel()
	s := sms.NewOptOutStore()
	s.OptOut("+61412345678")
	if !s.IsOptedOut("+61412345678") {
		t.Fatal("number should be opted out")
	}
}

func TestOptOutStore_NotOptedOut(t *testing.T) {
	t.Parallel()
	s := sms.NewOptOutStore()
	if s.IsOptedOut("+61400000000") {
		t.Fatal("number should not be opted out")
	}
}

// --- Service ---

func msg(to string) sms.Message {
	return sms.Message{To: to, From: "+60000000", Body: "Test message"}
}

func TestService_Send_Success(t *testing.T) {
	t.Parallel()
	p := sms.NewStubProvider("primary")
	svc := sms.NewService([]sms.Provider{p}, nil, nil)
	status, err := svc.Send(context.Background(), msg("+61412000001"))
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != sms.StatusSent {
		t.Fatalf("want sent, got %q", status.Status)
	}
}

func TestService_Send_Fallback(t *testing.T) {
	t.Parallel()
	primary := sms.NewStubProvider("primary")
	primary.Err = errors.New("provider down")
	fallback := sms.NewStubProvider("fallback")
	svc := sms.NewService([]sms.Provider{primary, fallback}, nil, nil)

	_, err := svc.Send(context.Background(), msg("+61412000002"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fallback.SentMessages()) != 1 {
		t.Fatal("fallback should have delivered the message")
	}
}

func TestService_Send_RateLimited(t *testing.T) {
	t.Parallel()
	rl := sms.NewRateLimiter(1, time.Minute)
	svc := sms.NewService([]sms.Provider{sms.NewStubProvider("p")}, rl, nil)
	_, _ = svc.Send(context.Background(), msg("+61412000003"))
	_, err := svc.Send(context.Background(), msg("+61412000003"))
	if !errors.Is(err, sms.ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
}

func TestService_Send_OptedOut(t *testing.T) {
	t.Parallel()
	optOuts := sms.NewOptOutStore()
	optOuts.OptOut("+61412999999")
	svc := sms.NewService([]sms.Provider{sms.NewStubProvider("p")}, nil, optOuts)
	status, err := svc.Send(context.Background(), msg("+61412999999"))
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != sms.StatusOptedOut {
		t.Fatalf("want opted_out, got %q", status.Status)
	}
}

func TestService_Send_AllProvidersFail(t *testing.T) {
	t.Parallel()
	p1 := sms.NewStubProvider("p1")
	p1.Err = errors.New("p1 down")
	p2 := sms.NewStubProvider("p2")
	p2.Err = errors.New("p2 down")
	svc := sms.NewService([]sms.Provider{p1, p2}, nil, nil)
	_, err := svc.Send(context.Background(), msg("+61412000004"))
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

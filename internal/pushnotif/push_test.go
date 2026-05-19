package pushnotif_test

import (
	"context"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/pushnotif"
)

func TestTopicStore_SubscribeUnsubscribe(t *testing.T) {
	t.Parallel()
	ts := pushnotif.NewTopicStore()
	ts.Subscribe("news", "token-a")
	ts.Subscribe("news", "token-b")
	subs := ts.Subscribers("news")
	if len(subs) != 2 {
		t.Fatalf("want 2 subscribers, got %d", len(subs))
	}
	ts.Unsubscribe("news", "token-a")
	if len(ts.Subscribers("news")) != 1 {
		t.Fatal("want 1 after unsubscribe")
	}
}

func TestTopicStore_EmptyTopic(t *testing.T) {
	t.Parallel()
	ts := pushnotif.NewTopicStore()
	if len(ts.Subscribers("empty")) != 0 {
		t.Fatal("empty topic should return no subscribers")
	}
}

func TestDispatcher_SendFCM(t *testing.T) {
	t.Parallel()
	ts := pushnotif.NewTopicStore()
	d := pushnotif.NewDispatcher(ts)
	p := pushnotif.NewStubProvider(pushnotif.PlatformFCM)
	d.Register(p)

	n := pushnotif.Notification{Token: "device-fcm-1", Platform: pushnotif.PlatformFCM, Title: "Hi", Body: "Hello"}
	result, err := d.Send(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}
}

func TestDispatcher_SendUnknownPlatform(t *testing.T) {
	t.Parallel()
	ts := pushnotif.NewTopicStore()
	d := pushnotif.NewDispatcher(ts)
	_, err := d.Send(context.Background(), pushnotif.Notification{Platform: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown platform")
	}
}

func TestDispatcher_Broadcast(t *testing.T) {
	t.Parallel()
	ts := pushnotif.NewTopicStore()
	ts.Subscribe("promo", "tok-1")
	ts.Subscribe("promo", "tok-2")
	ts.Subscribe("promo", "tok-3")

	d := pushnotif.NewDispatcher(ts)
	p := pushnotif.NewStubProvider(pushnotif.PlatformFCM)
	d.Register(p)

	results := d.Broadcast(context.Background(), "promo", pushnotif.Notification{Platform: pushnotif.PlatformFCM, Title: "Sale"})
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}
	if p.Delivered() != 3 {
		t.Fatalf("want 3 delivered, got %d", p.Delivered())
	}
}

func TestDispatcher_Broadcast_EmptyTopic(t *testing.T) {
	t.Parallel()
	ts := pushnotif.NewTopicStore()
	d := pushnotif.NewDispatcher(ts)
	results := d.Broadcast(context.Background(), "empty", pushnotif.Notification{Platform: pushnotif.PlatformFCM})
	if len(results) != 0 {
		t.Fatal("expected no results for empty topic")
	}
}

func TestBadgeManager_IncrementAndReset(t *testing.T) {
	t.Parallel()
	bm := pushnotif.NewBadgeManager()
	if bm.Count("tok") != 0 {
		t.Fatal("initial count should be 0")
	}
	bm.Increment("tok", 3)
	bm.Increment("tok", 2)
	if bm.Count("tok") != 5 {
		t.Fatalf("want 5, got %d", bm.Count("tok"))
	}
	bm.Reset("tok")
	if bm.Count("tok") != 0 {
		t.Fatal("count should be 0 after reset")
	}
}

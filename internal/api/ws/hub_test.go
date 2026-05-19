package ws_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/api/ws"
)

func TestWS_ClientConnectDisconnect(t *testing.T) {
	t.Parallel()
	h := ws.NewHub()
	c := ws.NewClient("C1", 10)
	h.Connect(c)
	h.Disconnect("C1")
}

func TestWS_SubscribeToTopic(t *testing.T) {
	t.Parallel()
	h := ws.NewHub()
	c := ws.NewClient("C1", 10)
	h.Connect(c)
	h.Subscribe("C1", "orders")
	h.Broadcast("orders", []byte("event"))
	select {
	case msg := <-c.Receive():
		if string(msg) != "event" {
			t.Fatalf("unexpected message: %s", msg)
		}
	default:
		t.Fatal("expected message on subscribed topic")
	}
}

func TestWS_BroadcastReachesSubscribers(t *testing.T) {
	t.Parallel()
	h := ws.NewHub()
	c1 := ws.NewClient("C1", 10)
	c2 := ws.NewClient("C2", 10)
	h.Connect(c1)
	h.Connect(c2)
	h.Subscribe("C1", "topic")
	h.Subscribe("C2", "topic")
	h.Broadcast("topic", []byte("hello"))
	for _, c := range []*ws.Client{c1, c2} {
		select {
		case <-c.Receive():
		default:
			t.Fatalf("expected message on client %s", c.ID)
		}
	}
}

func TestWS_BroadcastSkipsNonSubscribers(t *testing.T) {
	t.Parallel()
	h := ws.NewHub()
	c := ws.NewClient("C1", 10)
	h.Connect(c)
	// NOT subscribed
	h.Broadcast("other-topic", []byte("msg"))
	select {
	case <-c.Receive():
		t.Fatal("expected no message for non-subscriber")
	default:
	}
}

func TestWS_UnsubscribeStopsDelivery(t *testing.T) {
	t.Parallel()
	h := ws.NewHub()
	c := ws.NewClient("C1", 10)
	h.Connect(c)
	h.Subscribe("C1", "feed")
	h.Unsubscribe("C1", "feed")
	h.Broadcast("feed", []byte("nope"))
	select {
	case <-c.Receive():
		t.Fatal("expected no message after unsubscribe")
	default:
	}
}

func TestWS_ConcurrentBroadcastSafety(t *testing.T) {
	t.Parallel()
	h := ws.NewHub()
	for i := 0; i < 10; i++ {
		c := ws.NewClient("C"+string(rune('A'+i)), 100)
		h.Connect(c)
		h.Subscribe(c.ID, "concurrent")
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			h.Broadcast("concurrent", []byte("msg"))
		}
		close(done)
	}()
	<-done
}

func TestWS_HubShutdownDrainsClients(t *testing.T) {
	t.Parallel()
	h := ws.NewHub()
	c := ws.NewClient("C1", 10)
	h.Connect(c)
	h.Shutdown()
}

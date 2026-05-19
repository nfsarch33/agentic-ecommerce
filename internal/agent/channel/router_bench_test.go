package channel

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/workerpool"
)

func BenchmarkChannelRouter_FanOutToFourChannels(b *testing.B) {
	pool := workerpool.New(nil, workerpool.Config{Name: "router-bench", MaxWorkers: 4, QueueDepth: 32})
	defer func() { _ = pool.Close(context.Background()) }()
	bus := eventbus.NewInMemoryBus()
	defer func() { _ = bus.Close() }()
	channels := []ChannelDescriptor{
		{Adapter: newFakeAdapter("tiktok"), Matcher: MatchAlways},
		{Adapter: newFakeAdapter("facebook"), Matcher: MatchAlways},
		{Adapter: newFakeAdapter("rednote"), Matcher: MatchAlways},
		{Adapter: newFakeAdapter("instagram"), Matcher: MatchAlways},
	}
	router, err := NewChannelRouter(nil, ChannelRouterConfig{
		TenantID:        "tenant-1",
		Channels:        channels,
		Pool:            pool,
		Publisher:       bus,
		Now:             func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
		DispatchTimeout: time.Second,
	})
	if err != nil {
		b.Fatalf("NewChannelRouter: %v", err)
	}
	defer func() { _ = router.Close(context.Background()) }()
	t := &testing.T{}
	evt := enrichedRouterEvent(t, "p-bench", "any.cat")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = router.HandleEvent(context.Background(), evt)
	}
}

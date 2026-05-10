package fulfilment

import (
	"context"
	"testing"
)

type fakeUpdater struct{ name string }

func (f fakeUpdater) ChannelName() string                                              { return f.name }
func (f fakeUpdater) UpdateOrderStatus(_ context.Context, _ ChannelStatusUpdate) error { return nil }

func TestResolveDefaultChannels_DedupesByChannelName(t *testing.T) {
	t.Parallel()
	out := ResolveDefaultChannels(fakeUpdater{name: "tiktok"}, nil, fakeUpdater{name: "tiktok"}, fakeUpdater{name: "facebook"})
	if len(out) != 2 {
		t.Fatalf("expected 2 unique updaters, got %d", len(out))
	}
}

func TestResolveDefaultChannels_NilSafe(t *testing.T) {
	t.Parallel()
	out := ResolveDefaultChannels(nil, nil)
	if len(out) != 0 {
		t.Fatalf("expected 0, got %d", len(out))
	}
}

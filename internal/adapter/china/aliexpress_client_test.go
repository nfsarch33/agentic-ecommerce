package china

import (
	"context"
	"errors"
	"testing"
)

func TestAliExpressClient_StubModeRejectsLiveCalls(t *testing.T) {
	t.Parallel()
	client, err := NewAliExpressClient(nil, ConfigAliExpress{StubMode: true})
	if err != nil {
		t.Fatalf("NewAliExpressClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	if client.Source() != SourceAliExpress {
		t.Fatalf("Source = %s, want aliexpress", client.Source())
	}
	if !client.StubMode() {
		t.Fatalf("StubMode = false, want true")
	}
	if _, err := client.Search(context.Background(), SearchRequest{Keyword: "x"}); !errors.Is(err, ErrAliExpressStubMode) {
		t.Fatalf("Search stub: err=%v, want ErrAliExpressStubMode", err)
	}
	if _, err := client.ProductDetail(context.Background(), ProductDetailRequest{ExternalID: "x"}); !errors.Is(err, ErrAliExpressStubMode) {
		t.Fatalf("ProductDetail stub: err=%v, want ErrAliExpressStubMode", err)
	}
}

func TestAliExpressClient_RejectsEmptyInputs(t *testing.T) {
	t.Parallel()
	client, _ := NewAliExpressClient(nil, ConfigAliExpress{StubMode: true})
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	if _, err := client.Search(context.Background(), SearchRequest{}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("Search empty: err=%v, want ErrInvalidQuery", err)
	}
	if _, err := client.ProductDetail(context.Background(), ProductDetailRequest{}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("ProductDetail empty: err=%v, want ErrInvalidQuery", err)
	}
}

func TestAliExpressClient_LiveModeNotImplemented(t *testing.T) {
	t.Parallel()
	client, _ := NewAliExpressClient(nil, ConfigAliExpress{})
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	if _, err := client.Search(context.Background(), SearchRequest{Keyword: "x"}); err == nil {
		t.Fatal("Search live: want non-nil err (not implemented in v3.5.0)")
	}
}

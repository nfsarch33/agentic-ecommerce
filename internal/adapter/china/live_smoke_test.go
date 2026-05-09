//go:build live

package china

import (
	"context"
	"os"
	"testing"
)

func TestLive1688SearchSmokeRequiresOperatorSession(t *testing.T) {
	cookie := os.Getenv("ECOMMERCE_1688_SESSION_COOKIE")
	if cookie == "" {
		t.Skip("ECOMMERCE_1688_SESSION_COOKIE unset; operator-run live smoke only")
	}
	client, err := New1688Client(nil, Config1688{SessionCookie: cookie})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })

	products, err := client.Search(context.Background(), SearchRequest{Keyword: "wireless earbuds", MaxResults: 3})
	if err != nil {
		t.Fatalf("live 1688 search: %v", err)
	}
	if len(products) == 0 {
		t.Fatal("live 1688 search returned zero products")
	}
}

func TestLiveTaobaoSearchSmokeRequiresOperatorSession(t *testing.T) {
	cookie := os.Getenv("ECOMMERCE_TAOBAO_SESSION_COOKIE")
	if cookie == "" {
		t.Skip("ECOMMERCE_TAOBAO_SESSION_COOKIE unset; operator-run live smoke only")
	}
	client, err := NewTaobaoClient(nil, ConfigTaobao{SessionCookie: cookie})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })

	products, err := client.Search(context.Background(), SearchRequest{Keyword: "wireless earbuds", MaxResults: 3})
	if err != nil {
		t.Fatalf("live taobao search: %v", err)
	}
	if len(products) == 0 {
		t.Fatal("live taobao search returned zero products")
	}
}

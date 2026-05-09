package china

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/dnaeon/go-vcr.v3/cassette"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
)

const cassetteBase1688 = "https://cassette.1688.local"
const cassetteBaseTaobao = "https://cassette.taobao.local"

func TestAdapter1688_ReplaysSearchCassette(t *testing.T) {
	t.Parallel()

	rec := newReplayCassette(t, "testdata/cassettes/1688_search")
	client, err := New1688Client(nil, Config1688{
		BaseURL:       cassetteBase1688,
		SessionCookie: "session=redacted",
		HTTPClient:    rec.GetDefaultClient(),
		RateInterval:  time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New1688Client: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	products, err := client.Search(context.Background(), SearchRequest{Keyword: "wireless earbuds", MaxResults: 12})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(products) != 12 {
		t.Fatalf("products = %d, want 12", len(products))
	}
	first := products[0]
	if first.Source != Source1688 || first.ExternalID != "cassette-1688-earbud-01" {
		t.Fatalf("first product = %+v", first)
	}
	if first.PriceCNYCents != 1388 || first.MOQ != 20 || first.SupplierRating != 4.7 {
		t.Fatalf("first product missing cassette fields: %+v", first)
	}
}

func TestAdapterTaobao_Replays429BackoffCassette(t *testing.T) {
	t.Parallel()

	var sleeps []time.Duration
	rec := newReplayCassette(t, "testdata/cassettes/taobao_search_429_then_success")
	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:        cassetteBaseTaobao,
		SessionCookie:  "session=redacted",
		HTTPClient:     rec.GetDefaultClient(),
		BackoffInitial: 10 * time.Millisecond,
		BackoffMax:     40 * time.Millisecond,
		MaxRetries:     3,
		Sleep: func(d time.Duration) {
			sleeps = append(sleeps, d)
		},
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	products, err := client.Search(context.Background(), SearchRequest{Keyword: "wireless earbuds", MaxResults: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(products) != 2 {
		t.Fatalf("products = %d, want 2", len(products))
	}
	if got := sleeps; len(got) != 2 || got[0] != 10*time.Millisecond || got[1] != 20*time.Millisecond {
		t.Fatalf("backoff sleeps = %v, want [10ms 20ms]", got)
	}
	if products[0].Source != SourceTaobao || products[0].ExternalID != "cassette-taobao-earbud-01" {
		t.Fatalf("first product = %+v", products[0])
	}
}

func TestAdapterTaobao_ReplayCassetteExhausted429WrapsSentinel(t *testing.T) {
	t.Parallel()

	rec := newReplayCassette(t, "testdata/cassettes/taobao_search_429_exhausted")
	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:        cassetteBaseTaobao,
		SessionCookie:  "session=redacted",
		HTTPClient:     rec.GetDefaultClient(),
		BackoffInitial: time.Millisecond,
		BackoffMax:     time.Millisecond,
		MaxRetries:     2,
		Sleep:          func(time.Duration) {},
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	_, err = client.Search(context.Background(), SearchRequest{Keyword: "wireless earbuds", MaxResults: 3})
	if !errors.Is(err, ErrTaobaoRateLimited) {
		t.Fatalf("error = %v, want ErrTaobaoRateLimited", err)
	}
}

func TestAdapterTaobao_ReplaysDetailCassette(t *testing.T) {
	t.Parallel()

	rec := newReplayCassette(t, "testdata/cassettes/taobao_detail")
	client, err := NewTaobaoClient(nil, ConfigTaobao{
		BaseURL:       cassetteBaseTaobao,
		SessionCookie: "session=redacted",
		HTTPClient:    rec.GetDefaultClient(),
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	product, err := client.ProductDetail(context.Background(), ProductDetailRequest{ExternalID: "tb-vcr-123"})
	if err != nil {
		t.Fatalf("ProductDetail: %v", err)
	}
	if product.ExternalID != "tb-vcr-123" || product.Source != SourceTaobao || product.ReviewCount != 1240 {
		t.Fatalf("unexpected detail product: %+v", product)
	}
}

func TestChinaGoVCRCassettesContainNoSecrets(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"testdata/cassettes/1688_search.yaml",
		"testdata/cassettes/taobao_detail.yaml",
		"testdata/cassettes/taobao_search_429_then_success.yaml",
		"testdata/cassettes/taobao_search_429_exhausted.yaml",
	} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read cassette: %v", err)
			}
			lower := strings.ToLower(string(data))
			for _, marker := range []string{"session=", "cookie:", "authorization", "bearer ", "token"} {
				if strings.Contains(lower, marker) {
					t.Fatalf("cassette %s contains forbidden marker %q", path, marker)
				}
			}
		})
	}
}

func newReplayCassette(t *testing.T, cassetteName string) *recorder.Recorder {
	t.Helper()
	rec, err := recorder.NewWithOptions(&recorder.Options{
		CassetteName: cassetteName,
		Mode:         recorder.ModeReplayOnly,
	})
	if err != nil {
		t.Fatalf("new cassette recorder %s: %v", cassetteName, err)
	}
	rec.SetMatcher(matchReplayMethodPathAndQuery)
	t.Cleanup(func() {
		if err := rec.Stop(); err != nil {
			t.Errorf("stop cassette recorder %s: %v", cassetteName, err)
		}
	})
	return rec
}

func matchReplayMethodPathAndQuery(req *http.Request, recorded cassette.Request) bool {
	recordedURL, err := url.Parse(recorded.URL)
	if err != nil {
		return false
	}
	return req.Method == recorded.Method &&
		req.URL.Path == recordedURL.Path &&
		req.URL.RawQuery == recordedURL.RawQuery
}

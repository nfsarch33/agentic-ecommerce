// File scope: v6.1.0 coverage backfill -- PinterestAdapter ChannelName
// accessor was 0% covered.
package social

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPinterestAdapterChannelNameMatchesConstant(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newTestPinAdapter(t, srv.URL)
	if a.ChannelName() != PinterestChannelName {
		t.Fatalf("ChannelName() = %q, want %q", a.ChannelName(), PinterestChannelName)
	}
}

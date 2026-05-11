// File scope: v6.1.0 coverage backfill -- the simple ChannelName
// accessor on InstagramAdapter was 0% covered; the equivalent
// Pinterest/RedNote accessors land in their own _channelname_test
// files.
package social

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInstagramAdapterChannelNameMatchesConstant(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newTestIGAdapter(t, srv.URL)
	if a.ChannelName() != InstagramChannelName {
		t.Fatalf("ChannelName() = %q, want %q", a.ChannelName(), InstagramChannelName)
	}
}

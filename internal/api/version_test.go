package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/api"
)

func TestNegotiateVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		path   string
		accept string
		want   api.Version
	}{
		{name: "v1_default", path: "/api/v1/products", want: api.VersionV1},
		{name: "v2_path", path: "/api/v2/marketplace/plugins/foo/install", want: api.VersionV2Preview},
		{name: "v1_accept", path: "/api/v1/products", accept: api.MediaTypeV1, want: api.VersionV1},
		{name: "v2_accept", path: "/api/v1/products", accept: api.MediaTypeV2Preview, want: api.VersionV2Preview},
		{name: "v2_accept_with_quality", path: "/api/v1/products", accept: "text/html;q=0.5, application/vnd.ec.v2+json;q=1", want: api.VersionV2Preview},
		{name: "path_wins_over_accept", path: "/api/v2/x", accept: api.MediaTypeV1, want: api.VersionV2Preview},
		{name: "unknown_path_no_accept", path: "/healthz", want: api.VersionV1},
		{name: "ignored_accept", path: "/healthz", accept: "text/html", want: api.VersionV1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.accept != "" {
				req.Header.Set("Accept", tc.accept)
			}
			got := api.NegotiateVersion(req)
			if got != tc.want {
				t.Fatalf("NegotiateVersion(%q, accept=%q) = %s, want %s", tc.path, tc.accept, got, tc.want)
			}
		})
	}
}

func TestWithVersionHeadersStampsVersion(t *testing.T) {
	t.Parallel()
	handler := api.WithVersionHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name            string
		path            string
		wantVersion     string
		wantDeprecation bool
	}{
		{name: "v1_no_deprecation", path: "/api/v1/x", wantVersion: "1", wantDeprecation: false},
		{name: "v2_with_deprecation", path: "/api/v2/x", wantVersion: "2-preview", wantDeprecation: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			handler.ServeHTTP(rec, req)
			if got := rec.Header().Get(api.HeaderAPIVersion); got != tc.wantVersion {
				t.Fatalf("X-API-Version = %q, want %q", got, tc.wantVersion)
			}
			depr := rec.Header().Get(api.HeaderAPIDeprecation)
			if tc.wantDeprecation && depr == "" {
				t.Fatalf("X-API-Deprecation expected, got empty")
			}
			if !tc.wantDeprecation && depr != "" {
				t.Fatalf("X-API-Deprecation should be empty for stable, got %q", depr)
			}
		})
	}
}

func TestIsPreview(t *testing.T) {
	t.Parallel()
	if api.VersionV1.IsPreview() {
		t.Fatalf("VersionV1.IsPreview must be false")
	}
	if !api.VersionV2Preview.IsPreview() {
		t.Fatalf("VersionV2Preview.IsPreview must be true")
	}
}

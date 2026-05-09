package replay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplayRecorder_CapturesHTTPRoundtrip(t *testing.T) {
	t.Parallel()
	rec := NewRecorder("rt-test")
	rec.SetClock(func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("X-Test", "v1")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	body := []byte(`{"req":1}`)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/x", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	rec.Capture(req, body, resp, respBody)
	c := rec.Cassette()
	if len(c.HTTP) != 1 {
		t.Fatalf("want 1 interaction, got %d", len(c.HTTP))
	}
	if c.HTTP[0].Status != http.StatusCreated {
		t.Fatalf("want 201, got %d", c.HTTP[0].Status)
	}
	if c.HTTP[0].Headers["X-Test"] != "v1" {
		t.Fatalf("missing X-Test header in %v", c.HTTP[0].Headers)
	}
}

func TestReplayRecorder_PersistsToYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rec := NewRecorder("persist")
	rec.SetClock(func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) })
	rec.CaptureDOM("step-1", `<html>hi</html>`)
	rec.CaptureEvent("click", "#submit", "")
	path := filepath.Join(dir, "out.yaml")
	if err := rec.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadCassette(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Name != "persist" {
		t.Fatalf("name lost: %q", loaded.Name)
	}
	if len(loaded.DOMSnapshots) != 1 {
		t.Fatalf("want 1 DOM, got %d", len(loaded.DOMSnapshots))
	}
	if len(loaded.UIEvents) != 1 || loaded.UIEvents[0].Selector != "#submit" {
		t.Fatalf("UI event lost: %v", loaded.UIEvents)
	}
}

func TestReplayPlayer_DeterministicReplay(t *testing.T) {
	t.Parallel()
	c := Cassette{
		Version: CassetteVersion,
		Name:    "det",
		HTTP: []HTTPInteraction{
			{Method: http.MethodGet, URL: "https://example.com/a", Status: 200, ResponseBody: "first"},
			{Method: http.MethodPost, URL: "https://example.com/b", Status: 201, ResponseBody: "second"},
		},
	}
	p := NewPlayer(c)
	for _, want := range []struct {
		method string
		url    string
		body   string
	}{
		{http.MethodGet, "https://example.com/a", "first"},
		{http.MethodPost, "https://example.com/b", "second"},
	} {
		req, _ := http.NewRequestWithContext(context.Background(), want.method, want.url, nil)
		resp, err := p.Next(context.Background(), req)
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		gotBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(gotBody) != want.body {
			t.Fatalf("want body %q, got %q", want.body, gotBody)
		}
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/a", nil)
	if _, err := p.Next(context.Background(), req); !errors.Is(err, ErrPlaybackExhausted) {
		t.Fatalf("want ErrPlaybackExhausted, got %v", err)
	}
}

func TestReplayPlayer_DetectsMismatch(t *testing.T) {
	t.Parallel()
	c := Cassette{
		Version: CassetteVersion,
		HTTP: []HTTPInteraction{
			{Method: http.MethodGet, URL: "https://example.com/a", Status: 200},
		},
	}
	p := NewPlayer(c)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/different", nil)
	if _, err := p.Next(context.Background(), req); !errors.Is(err, ErrPlaybackMismatch) {
		t.Fatalf("want ErrPlaybackMismatch, got %v", err)
	}
}

func TestReplayHarness_EndToEndRedNoteCassette(t *testing.T) {
	t.Parallel()
	cassettePath := filepath.Join("..", "..", "..", "tests", "uiauto", "cassettes", "rednote_post_creation.yaml")
	c, err := LoadCassette(cassettePath)
	if err != nil {
		t.Fatalf("load rednote cassette: %v", err)
	}
	if c.Name == "" {
		t.Fatalf("cassette name missing")
	}
	if len(c.HTTP) == 0 {
		t.Fatalf("cassette HTTP empty")
	}
	p := NewPlayer(c)
	first := c.HTTP[0]
	req, _ := http.NewRequestWithContext(context.Background(), first.Method, first.URL, strings.NewReader(first.RequestBody))
	resp, err := p.Next(context.Background(), req)
	if err != nil {
		t.Fatalf("first replay: %v", err)
	}
	if resp.StatusCode != first.Status {
		t.Fatalf("status mismatch: %d vs %d", resp.StatusCode, first.Status)
	}
	resp.Body.Close()
}

func TestLoadCassette_RejectsMissing(t *testing.T) {
	t.Parallel()
	if _, err := LoadCassette(filepath.Join(t.TempDir(), "missing.yaml")); !errors.Is(err, ErrCassetteNotFound) {
		t.Fatalf("want ErrCassetteNotFound, got %v", err)
	}
}

func TestLoadCassette_RejectsBadYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	_ = mustWrite(t, path, "::: not yaml :::")
	if _, err := LoadCassette(path); !errors.Is(err, ErrCassetteCorrupted) {
		t.Fatalf("want ErrCassetteCorrupted, got %v", err)
	}
}

func TestLoadCassette_RejectsMissingVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "noversion.yaml")
	_ = mustWrite(t, path, "name: x\n")
	if _, err := LoadCassette(path); !errors.Is(err, ErrCassetteCorrupted) {
		t.Fatalf("want ErrCassetteCorrupted, got %v", err)
	}
}

func TestPlayerTransport_RoundTrips(t *testing.T) {
	t.Parallel()
	c := Cassette{
		Version: CassetteVersion,
		HTTP: []HTTPInteraction{
			{Method: http.MethodGet, URL: "https://example.com/a", Status: 200, ResponseBody: "hi"},
		},
	}
	p := NewPlayer(c)
	client := &http.Client{Transport: &PlayerTransport{Player: p}}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/a", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "hi" {
		t.Fatalf("body mismatch: %q", got)
	}
}

func TestPlayer_Reset(t *testing.T) {
	t.Parallel()
	c := Cassette{
		Version: CassetteVersion,
		HTTP: []HTTPInteraction{
			{Method: http.MethodGet, URL: "https://example.com/a", Status: 200},
		},
	}
	p := NewPlayer(c)
	if c := p.Cursor(); c != 0 {
		t.Fatalf("want cursor 0, got %d", c)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/a", nil)
	if _, err := p.Next(context.Background(), req); err != nil {
		t.Fatalf("next: %v", err)
	}
	if c := p.Cursor(); c != 1 {
		t.Fatalf("want cursor 1, got %d", c)
	}
	p.Reset()
	if c := p.Cursor(); c != 0 {
		t.Fatalf("want cursor 0 after reset, got %d", c)
	}
}

func mustWrite(t *testing.T, path, body string) error {
	t.Helper()
	if err := writeFile(path, body); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return nil
}

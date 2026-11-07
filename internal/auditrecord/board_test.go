package auditrecord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPBoardListsBothShapesAndPostsWithTheBearerInTheHeaderOnly(t *testing.T) {
	var posted map[string]string
	bare := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.EscapedPath() != "/api/tickets/T%201/comments" {
			t.Errorf("path = %s, want the ticket id path-escaped", r.URL.EscapedPath())
		}
		switch r.Method {
		case http.MethodGet:
			if bare {
				_, _ = io.WriteString(w, `[{"body":"b1","created_at":"2026-11-07T01:02:03Z"}]`)
				return
			}
			_, _ = io.WriteString(w, `{"ticket_id":"T 1","comments":[{"body":"a1","created_at":"2026-11-07T01:02:03Z"},{"body":"a2","created_at":"2026-11-07T02:02:03Z"}]}`)
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer srv.Close()
	b := HTTPBoard{BaseURL: srv.URL + "/api/", Token: "s3cret", Author: "listing-job"}

	cs, err := b.Comments(context.Background(), "T 1")
	if err != nil || len(cs) != 2 || cs[1].Body != "a2" || cs[1].CreatedAt.Hour() != 2 {
		t.Fatalf("wrapped = %+v, %v", cs, err)
	}
	bare = true
	cs, err = b.Comments(context.Background(), "T 1")
	if err != nil || len(cs) != 1 || cs[0].Body != "b1" {
		t.Fatalf("bare = %+v, %v", cs, err)
	}
	if err := b.Comment(context.Background(), "T 1", "hello"); err != nil {
		t.Fatal(err)
	}
	if posted["author"] != "listing-job" || posted["body"] != "hello" {
		t.Fatalf("posted = %v", posted)
	}
}

func TestHTTPBoardErrorNamesTheStatusButNeverTheToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()
	b := HTTPBoard{BaseURL: srv.URL, Token: "s3cret"}
	err := b.Comment(context.Background(), "T", "x")
	if err == nil || !strings.Contains(err.Error(), "http 500") || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("err = %v, want http 500 without the token", err)
	}
	if _, err := b.Comments(context.Background(), "T"); err == nil {
		t.Fatal("list on a 500 returned no error")
	}
}

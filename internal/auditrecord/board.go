package auditrecord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPBoard is a Board over a sprint board's REST API:
//
//	GET  {base}/tickets/{id}/comments  -> {"comments":[{"body":..,"created_at":..}]} (or a bare array)
//	POST {base}/tickets/{id}/comments  <- {"author":..,"body":..}
//
// The bearer rides the Authorization header only; it is never logged and
// never part of an error.
type HTTPBoard struct {
	BaseURL string
	Token   string
	Author  string
	Client  *http.Client
}

type apiComment struct {
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

func (b HTTPBoard) url(ticket string) string {
	return strings.TrimRight(b.BaseURL, "/") + "/tickets/" + url.PathEscape(ticket) + "/comments"
}

func (b HTTPBoard) client() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (b HTTPBoard) do(req *http.Request) ([]byte, error) {
	req.Header.Set("Authorization", "Bearer "+b.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := b.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("board %s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("board %s %s: http %d: %s", req.Method, req.URL.Path, resp.StatusCode, oneLine(string(body[:min(len(body), 200)])))
	}
	return body, nil
}

// Comments implements Board.
func (b HTTPBoard) Comments(ctx context.Context, ticket string) ([]Comment, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.url(ticket), nil)
	if err != nil {
		return nil, err
	}
	raw, err := b.do(req)
	if err != nil {
		return nil, err
	}
	var list []apiComment
	var wrapped struct {
		Comments []apiComment `json:"comments"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Comments != nil {
		list = wrapped.Comments
	} else if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("board comments for %s: unexpected body", ticket)
	}
	out := make([]Comment, 0, len(list))
	for _, c := range list {
		out = append(out, Comment(c))
	}
	return out, nil
}

// Comment implements Board.
func (b HTTPBoard) Comment(ctx context.Context, ticket, body string) error {
	payload, err := json.Marshal(map[string]string{"author": b.Author, "body": body})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.url(ticket), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	_, err = b.do(req)
	return err
}

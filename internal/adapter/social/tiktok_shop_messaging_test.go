package social

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

func TestTikTokShopClient_SendMessageSucceeds(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/messages/send" {
			t.Errorf("path = %s, want /api/messages/send", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("unmarshal: %v", err)
		}
		if body["thread_id"] != "thread-123" {
			t.Errorf("thread_id = %v", body["thread_id"])
		}
		if body["text"] != "hello" {
			t.Errorf("text = %v", body["text"])
		}
		if r.Header.Get("X-Tts-Sign") == "" {
			t.Errorf("missing X-Tts-Sign header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"message_id": "msg-99"})
	}))
	defer srv.Close()
	client := newClientWithBaseURL(t, srv.URL, srv.Client(), nil)
	resp, err := client.SendMessage(context.Background(), port.OutboundMessageRequest{
		TenantID: "tenant-test",
		ThreadID: "thread-123",
		Text:     "hello",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.ProviderMessageID != "msg-99" {
		t.Fatalf("ProviderMessageID = %s", resp.ProviderMessageID)
	}
}

func TestTikTokShopClient_SendMessageRejectsEmptyArgs(t *testing.T) {
	t.Parallel()
	client := newClientWithBaseURL(t, "http://localhost:9", &http.Client{}, nil)
	cases := map[string]port.OutboundMessageRequest{
		"missing_thread": {Text: "hi"},
		"missing_text":   {ThreadID: "t"},
	}
	for name, req := range cases {
		name, req := name, req
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := client.SendMessage(context.Background(), req)
			if !errors.Is(err, ErrTikTokUnconfigured) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestTikTokShopClient_SendMessageRejectsClosed(t *testing.T) {
	t.Parallel()
	client := newClientWithBaseURL(t, "http://localhost:9", &http.Client{}, nil)
	_ = client.Close(context.Background())
	_, err := client.SendMessage(context.Background(), port.OutboundMessageRequest{ThreadID: "t", Text: "hi"})
	if !errors.Is(err, ErrTikTokClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestTikTokShopClient_SendMessageSurfaces5xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal"))
	}))
	defer srv.Close()
	client := newClientWithBaseURL(t, srv.URL, srv.Client(), nil)
	_, err := client.SendMessage(context.Background(), port.OutboundMessageRequest{ThreadID: "t", Text: "hi"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("err = %v, want status 500", err)
	}
}

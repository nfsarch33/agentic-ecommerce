package social

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

func TestFacebookShopClient_SendMessageSucceeds(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("unmarshal: %v", err)
		}
		recipient, _ := body["recipient"].(map[string]any)
		if recipient["id"] != "psid-789" {
			t.Errorf("recipient id = %v", recipient["id"])
		}
		message, _ := body["message"].(map[string]any)
		if message["text"] != "hi from FB" {
			t.Errorf("text = %v", message["text"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"message_id": "fb-msg-1"})
	}))
	defer srv.Close()

	client := mustNewFakeFBClient(t, srv.URL, srv.Client(), nil)
	resp, err := client.SendMessage(context.Background(), port.OutboundMessageRequest{
		TenantID: "tenant-fb",
		ThreadID: "psid-789",
		Text:     "hi from FB",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.ProviderMessageID != "fb-msg-1" {
		t.Fatalf("ProviderMessageID = %s", resp.ProviderMessageID)
	}
}

func TestFacebookShopClient_SendMessageRejectsEmptyArgs(t *testing.T) {
	t.Parallel()
	client := mustNewFakeFBClient(t, "http://localhost:9", &http.Client{}, nil)
	cases := map[string]port.OutboundMessageRequest{
		"missing_thread": {Text: "hi"},
		"missing_text":   {ThreadID: "t"},
	}
	for name, req := range cases {
		name, req := name, req
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := client.SendMessage(context.Background(), req)
			if !errors.Is(err, ErrFacebookUnconfigured) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFacebookShopClient_SendMessageRejectsClosed(t *testing.T) {
	t.Parallel()
	client := mustNewFakeFBClient(t, "http://localhost:9", &http.Client{}, nil)
	_ = client.Close(context.Background())
	_, err := client.SendMessage(context.Background(), port.OutboundMessageRequest{ThreadID: "t", Text: "hi"})
	if !errors.Is(err, ErrFacebookClosed) {
		t.Fatalf("err = %v", err)
	}
}

package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

type stubEinoProvider struct {
	requests []port.AICompletionRequest
	response port.AICompletionResponse
	err      error
}

func (s *stubEinoProvider) Complete(_ context.Context, req port.AICompletionRequest) (port.AICompletionResponse, error) {
	s.requests = append(s.requests, req)
	if s.err != nil {
		return port.AICompletionResponse{}, s.err
	}
	return s.response, nil
}

func (s *stubEinoProvider) Name() string { return "stub" }

func TestNewEinoAdapterImplementsToolCallingChatModel(t *testing.T) {
	t.Parallel()

	var _ model.ToolCallingChatModel = NewEinoAdapter(&stubEinoProvider{})
}

func TestEinoAdapterGenerateMapsMessagesAndUsage(t *testing.T) {
	t.Parallel()

	stub := &stubEinoProvider{
		response: port.AICompletionResponse{
			Content:    "generated copy",
			TokensUsed: 17,
		},
	}
	adapter := NewEinoAdapter(stub)

	temperature := float32(0.7)
	maxTokens := 180
	msg, err := adapter.Generate(
		context.Background(),
		[]*schema.Message{
			schema.SystemMessage("You are a merchandising copilot."),
			schema.UserMessage("Write a concise product summary."),
		},
		model.WithModel("minimax-latest"),
		model.WithTemperature(temperature),
		model.WithMaxTokens(maxTokens),
	)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if got := len(stub.requests); got != 1 {
		t.Fatalf("provider requests = %d, want 1", got)
	}
	req := stub.requests[0]
	if req.Model != "minimax-latest" {
		t.Fatalf("request model = %q, want minimax-latest", req.Model)
	}
	if req.Temperature == nil || *req.Temperature != float64(temperature) {
		t.Fatalf("request temperature = %#v, want %v", req.Temperature, temperature)
	}
	if req.MaxTokens == nil || *req.MaxTokens != maxTokens {
		t.Fatalf("request max tokens = %#v, want %d", req.MaxTokens, maxTokens)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("request messages = %d, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != string(schema.System) || req.Messages[0].Content != "You are a merchandising copilot." {
		t.Fatalf("system message = %#v", req.Messages[0])
	}
	if req.Messages[1].Role != string(schema.User) || req.Messages[1].Content != "Write a concise product summary." {
		t.Fatalf("user message = %#v", req.Messages[1])
	}

	if msg.Role != schema.Assistant {
		t.Fatalf("response role = %q, want assistant", msg.Role)
	}
	if msg.Content != "generated copy" {
		t.Fatalf("response content = %q, want generated copy", msg.Content)
	}
	if msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil || msg.ResponseMeta.Usage.TotalTokens != 17 {
		t.Fatalf("response usage = %#v, want total_tokens=17", msg.ResponseMeta)
	}
}

func TestEinoAdapterWithToolsDoesNotMutateReceiver(t *testing.T) {
	t.Parallel()

	adapter := NewEinoAdapter(&stubEinoProvider{})
	original, ok := adapter.(*EinoModelAdapter)
	if !ok {
		t.Fatalf("adapter type = %T, want *EinoModelAdapter", adapter)
	}

	tools := []*schema.ToolInfo{{Name: "lookup", Desc: "Look up catalog facts"}}
	withTools, err := adapter.WithTools(tools)
	if err != nil {
		t.Fatalf("WithTools returned error: %v", err)
	}
	bound, ok := withTools.(*EinoModelAdapter)
	if !ok {
		t.Fatalf("withTools type = %T, want *EinoModelAdapter", withTools)
	}

	tools[0].Name = "mutated"
	if len(original.tools) != 0 {
		t.Fatalf("original adapter tools = %d, want 0", len(original.tools))
	}
	if len(bound.tools) != 1 || bound.tools[0].Name != "lookup" {
		t.Fatalf("bound tools = %#v, want copied lookup tool", bound.tools)
	}
}

func TestEinoAdapterStreamWrapsGenerate(t *testing.T) {
	t.Parallel()

	adapter := NewEinoAdapter(&stubEinoProvider{
		response: port.AICompletionResponse{
			Content:    "streamed",
			TokensUsed: 5,
		},
	})

	reader, err := adapter.Stream(context.Background(), []*schema.Message{schema.UserMessage("ping")})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	msg, err := schema.ConcatMessageStream(reader)
	if err != nil {
		t.Fatalf("ConcatMessageStream returned error: %v", err)
	}
	if msg.Content != "streamed" {
		t.Fatalf("stream content = %q, want streamed", msg.Content)
	}
}

func TestEinoAdapterGeneratePropagatesProviderError(t *testing.T) {
	t.Parallel()

	adapter := NewEinoAdapter(&stubEinoProvider{err: errors.New("boom")})

	_, err := adapter.Generate(context.Background(), []*schema.Message{schema.UserMessage("ping")})
	if err == nil {
		t.Fatal("Generate returned nil error")
	}
	if err.Error() != "eino adapter: provider.Complete: boom" {
		t.Fatalf("Generate error = %q, want wrapped provider error", err)
	}
}

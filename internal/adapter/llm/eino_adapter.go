package llm

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

// EinoModelAdapter bridges the existing provider-neutral completion port into
// EINO's tool-calling chat model surface.
type EinoModelAdapter struct {
	provider Provider
	tools    []*schema.ToolInfo
}

var _ model.ToolCallingChatModel = (*EinoModelAdapter)(nil)

// NewEinoAdapter wraps the existing failover/provider surface as an immutable
// EINO ToolCallingChatModel.
func NewEinoAdapter(provider Provider) model.ToolCallingChatModel {
	return &EinoModelAdapter{provider: provider}
}

// WithTools returns a new adapter instance with copied tool bindings.
func (a *EinoModelAdapter) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &EinoModelAdapter{
		provider: a.provider,
		tools:    cloneToolInfos(tools),
	}, nil
}

// Generate adapts EINO chat messages and common model options onto the current
// provider-neutral completion port.
func (a *EinoModelAdapter) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	common := model.GetCommonOptions(nil, opts...)
	req := port.AICompletionRequest{
		Messages: messagesFromSchema(input),
	}
	if common.Model != nil {
		req.Model = *common.Model
	}
	if common.Temperature != nil {
		temperature := float64(*common.Temperature)
		req.Temperature = &temperature
	}
	if common.MaxTokens != nil {
		maxTokens := *common.MaxTokens
		req.MaxTokens = &maxTokens
	}

	resp, err := a.provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("eino adapter: provider.Complete: %w", err)
	}

	msg := &schema.Message{
		Role:    schema.Assistant,
		Content: resp.Content,
	}
	if resp.TokensUsed > 0 {
		msg.ResponseMeta = &schema.ResponseMeta{
			Usage: &schema.TokenUsage{TotalTokens: resp.TokensUsed},
		}
	}
	return msg, nil
}

// Stream exposes the existing non-streaming provider through an EINO stream.
func (a *EinoModelAdapter) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := a.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func messagesFromSchema(input []*schema.Message) []port.AIMessage {
	if len(input) == 0 {
		return nil
	}

	messages := make([]port.AIMessage, 0, len(input))
	for _, msg := range input {
		if msg == nil {
			continue
		}
		messages = append(messages, port.AIMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}
	return messages
}

func cloneToolInfos(tools []*schema.ToolInfo) []*schema.ToolInfo {
	if len(tools) == 0 {
		return nil
	}

	cloned := make([]*schema.ToolInfo, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			cloned = append(cloned, nil)
			continue
		}
		copy := *tool
		cloned = append(cloned, &copy)
	}
	return cloned
}

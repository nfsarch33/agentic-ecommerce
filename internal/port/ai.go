package port

import "context"

// AIMessage is a provider-neutral chat message for text generation ports.
type AIMessage struct {
	Role    string
	Content string
}

// AICompletionRequest carries provider-neutral chat completion inputs.
type AICompletionRequest struct {
	Model       string
	Messages    []AIMessage
	Temperature *float64
	MaxTokens   *int
}

// AICompletionResponse is the normalized text generation result.
type AICompletionResponse struct {
	Content    string
	TokensUsed int
}

// AITextGenerator is implemented by adapters that can generate copy.
type AITextGenerator interface {
	Complete(ctx context.Context, req AICompletionRequest) (AICompletionResponse, error)
}

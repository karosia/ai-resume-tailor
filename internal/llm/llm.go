package llm

import "context"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

// Request is a provider-agnostic completion request. Each provider translates
// this into its own wire format.
type Request struct {
	System      string
	Messages    []Message
	MaxTokens   int
	Temperature float64
}

type Response struct {
	Content  string
	Provider string
	Model    string
}

// Provider is the entire extensibility surface
type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request) (*Response, error)
}

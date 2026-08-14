// Package llm defines a provider-agnostic interface for calling language
// models, plus a Client that tries multiple providers in order (failover).
//
// The whole point of this package: the rest of the app depends ONLY on the
// Provider interface and the Client. It never imports "anthropic" or "openai"
// directly. Adding a new provider = writing one new file that satisfies
// Provider, then registering it. Nothing else changes.
package llm

import (
	"context"
	"time"
)

// providerHTTPTimeout is a per-call safety net for provider HTTP clients.
//
// The caller's context.Context is the real deadline for any request. This
// value is deliberately larger than any outer timeout the app sets, so that
// context cancellation — not this timeout — is what actually stops a slow
// call. It exists only as a last resort for the case where a caller forgets
// to set a deadline at all. Keep it strictly greater than the app's overall
// request budget (see main.go).
const providerHTTPTimeout = 120 * time.Second

// Role identifies who authored a message in a conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single turn in a conversation.
type Message struct {
	Role    Role
	Content string
}

// Request is a provider-agnostic completion request. Each provider is
// responsible for translating this into its own wire format.
type Request struct {
	System      string    // optional system prompt
	Messages    []Message // conversation so far (at least one user message)
	MaxTokens   int       // upper bound on output tokens
	Temperature float64   // 0.0 = deterministic-ish, higher = more random
}

// Response is what a provider returns on success.
type Response struct {
	Content    string // the model's text output
	Provider   string // which provider produced this (e.g. "anthropic")
	Model      string // the concrete model id used (e.g. "claude-sonnet-5")
	StopReason string // why generation stopped ("stop"/"end_turn" = complete; "max_tokens" = truncated)
}

// Truncated reports whether the response was cut off because it hit the token
// limit, rather than finishing naturally. Callers that parse structured output
// (JSON) use this to give a clear "raise MaxTokens" error instead of a confusing
// "unexpected end of JSON input".
func (r *Response) Truncated() bool {
	switch r.StopReason {
	case "max_tokens", "length":
		return true // "max_tokens" = Anthropic, "length" = OpenAI
	default:
		return false
	}
}

// Provider is the contract every LLM backend must satisfy. This is the entire
// extensibility surface — implement these two methods and you're a provider.
type Provider interface {
	// Name is a short, stable identifier used in logs and errors.
	Name() string
	// Complete performs one request/response round trip. It must respect
	// ctx cancellation and return a non-nil error on any failure.
	Complete(ctx context.Context, req Request) (*Response, error)
}

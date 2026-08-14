package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// AnthropicProvider calls the Anthropic Messages API.
// Docs: https://docs.claude.com/en/api/messages
type AnthropicProvider struct {
	apiKey string
	model  string
	http   *http.Client
}

// NewAnthropicProvider constructs the provider. If model is empty, a sensible
// default is used. The caller owns the API key (read it from the environment).
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	if model == "" {
		model = "claude-sonnet-5"
	}
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		// Safety net only — the request's context is the real deadline.
		http: &http.Client{Timeout: providerHTTPTimeout},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

// --- wire types: shapes that match the Anthropic JSON API exactly ---

type anthropicReqBody struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	// Temperature/top_p/top_k are intentionally omitted: Claude Opus 4.7+ and
	// Sonnet 5 reject non-default sampling parameters with a 400. Behavior is
	// controlled through the system prompt instead.
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRespBody struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (p *AnthropicProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("anthropic: missing API key")
	}

	// Translate our provider-agnostic Request into Anthropic's wire format.
	body := anthropicReqBody{
		Model:     p.model,
		MaxTokens: req.MaxTokens,
		System:    req.System,
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, anthropicMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: http call: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, data)
	}

	var out anthropicRespBody
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("anthropic: decode body: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("anthropic: api error: %s", out.Error.Message)
	}

	// The Messages API returns content as an array of blocks; concatenate the
	// text blocks into a single string.
	var text string
	for _, block := range out.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	if text == "" {
		return nil, fmt.Errorf("anthropic: empty response content")
	}

	return &Response{Content: text, Provider: p.Name(), Model: out.Model, StopReason: out.StopReason}, nil
}

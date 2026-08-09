package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type AnthropicProvider struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewAnthropicProvider(apikey, model string) *AnthropicProvider {
	if model == "" {
		model = "claude-sonnet-5"
	}
	return &AnthropicProvider{
		apiKey: apikey,
		model:  model,
		http:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type anthropicReqBody struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Temperature float64            `json:"temperature"`
	Messages    []anthropicMessage `json:"messages"`
}

type anthropicRespBody struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model string `json:"model"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (p *AnthropicProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("anthropic: missing API key")
	}

	body := anthropicReqBody{
		Model:       p.model,
		MaxTokens:   req.MaxTokens,
		System:      req.System,
		Temperature: req.Temperature,
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-1")

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: http call: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: status: %d: %s", resp.StatusCode, data)
	}

	var out anthropicRespBody
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("anthropic: decode body: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("anthropic: api error: %s", out.Error.Message)
	}

	var text string
	for _, block := range out.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	if text == "" {
		return nil, fmt.Errorf("anthropic: empty response content")
	}

	return &Response{Content: text, Provider: p.Name(), Model: out.Model}, nil
}

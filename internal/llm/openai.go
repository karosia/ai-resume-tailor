package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type OpenAIProvider struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewOpenAIProvider(apiKey, model string) *OpenAIProvider {
	if model == "" {
		model = "gpt-5.5"
	}
	return &OpenAIProvider{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: providerHTTPTimeout},
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiReqBody struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float64         `json:"temperature"`
	Messages    []openaiMessage `json:"messages"`
}

type openaiRespBody struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Model string `json:"model"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *OpenAIProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("openai: missing api key")
	}

	// OpenAI has no separate "system" field — it's a message with role=system,
	var msgs []openaiMessage
	if req.System != "" {
		msgs = append(msgs, openaiMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, openaiMessage{Role: string(m.Role), Content: m.Content})
	}

	body := openaiReqBody{
		Model:       p.model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Messages:    msgs,
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("authorization", fmt.Sprintf("Bearer %s", p.apiKey))

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: http call: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: status %d: %s", resp.StatusCode, data)
	}

	var out openaiRespBody
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("openai: decode body: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("openai: api error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 || out.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("openai: empty response content")
	}

	return &Response{
		Content:  out.Choices[0].Message.Content,
		Provider: p.Name(),
		Model:    out.Model,
	}, nil
}

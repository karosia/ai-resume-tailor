package llm

import (
	"net/http"
)

// GeminiProvider calls Google's Gemini API (generateContent).
// Docs: https://ai.google.dev/api/generate-content
//
// Like anthropic.go and openai.go, this implements the same Provider interface,
// so the Client can fail over to it without knowing anything Gemini-specific.
// Gemini has three quirks the others don't: the model name goes in the URL path
// (not the body), the system prompt is a dedicated "systemInstruction" field,
// and the assistant role is called "model".
type GeminiProvider struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	if model == "" {
		model = "gemini-3.6-flash"
	}
	return &GeminiProvider{
		apiKey: apiKey,
		model:  model,
		// Safety net only — the request's context is the real deadline.
		http: &http.Client{Timeout: providerHTTPTimeout},
	}
}

func (p *GeminiProvider) Name() string { return "gemini" }

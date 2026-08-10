// Package jsonx contains small helpers for coaxing structured JSON out of LLM
// responses, which often arrive wrapped in markdown fences or stray prose.
package jsonx

import "strings"

// Extract pulls the JSON payload out of a model response. Models sometimes wrap
// JSON in ```json fences or add a sentence around it; this strips both. If no
// JSON object is found, the input is returned unchanged so the caller's parse
// step fails loudly rather than silently swallowing the problem.
func Extract(s string) string {
	s = strings.TrimSpace(s)

	// Strip a leading ```json / ``` fence and its closing fence if present.
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl != -1 {
			s = s[nl+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}

	// As a fallback, carve out the outermost JSON object.
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start != -1 && end != -1 && end > start {
		return s[start : end+1]
	}
	return s
}

// Package jsonx contains small helpers for coaxing structured JSON out of LLM
// responses, which often arrive wrapped in markdown fences or stray prose.
package jsonx

import "strings"

func Extract(s string) string {
	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl != -1 {
			s = s[nl+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}

	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start != -1 && end != -1 && end > start {
		return s[start : end+1]
	}
	return s
}

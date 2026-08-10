// Package jd analyzes a raw job description into structured requirements:
// title, seniority, required skills, nice-to-haves, ATS keywords, and
// responsibilities. Downstream, the tailor package matches resume items against
// these terms and (later) assembles a tailored resume.
package jd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ai-resume-tailor/internal/jsonx"
	"ai-resume-tailor/internal/llm"
)

// JD is the structured form of a job description. Struct tags define the exact
// JSON the model is asked to return, so its output unmarshals straight in.
type JD struct {
	Title            string   `json:"title"`
	Seniority        string   `json:"seniority"`
	RequiredSkills   []string `json:"required_skills"`
	NiceToHave       []string `json:"nice_to_have"`
	Keywords         []string `json:"keywords"`
	Responsibilities []string `json:"responsibilities"`
}

// completer is the slice of llm.Client the Analyzer needs (see resume package
// for the same pattern) — it keeps analysis unit-testable without the network.
type completer interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Analyzer turns raw JD text into a structured JD.
type Analyzer struct {
	llm completer
	log *slog.Logger
}

func NewAnalyzer(c completer, log *slog.Logger) *Analyzer {
	if log == nil {
		log = slog.Default()
	}
	return &Analyzer{llm: c, log: log}
}

const analyzeSystemPrompt = `You are a precise job-description analyzer. Given the raw text of a job posting, extract its requirements into structured fields.

Rules:
- "required_skills": concrete skills/technologies the role clearly requires (e.g. "Go", "Kubernetes", "distributed systems").
- "nice_to_have": skills mentioned as preferred, bonus, or a plus.
- "keywords": additional ATS-relevant terms a resume should echo — tools, domains, methods, and role-specific nouns that appear in the posting.
- "responsibilities": short phrases describing what the person will do.
- "seniority": one lowercase word if stated or clearly implied (e.g. "junior", "mid", "senior", "staff"); otherwise "".
- Extract only terms actually present or clearly implied in the posting. Do not invent requirements.
- Prefer short, canonical terms ("Go", not "the Go programming language").
- Return ONLY a JSON object. No prose, no markdown code fences.

Schema:
{"title":"...","seniority":"...","required_skills":["..."],"nice_to_have":["..."],"keywords":["..."],"responsibilities":["..."]}`

// Analyze sends the JD text to the LLM and returns structured requirements.
func (a *Analyzer) Analyze(ctx context.Context, jdText string) (*JD, error) {
	jdText = strings.TrimSpace(jdText)
	if jdText == "" {
		return nil, fmt.Errorf("jd: empty job description")
	}

	resp, err := a.llm.Complete(ctx, llm.Request{
		System:      analyzeSystemPrompt,
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: jdText}},
		MaxTokens:   2048,
		Temperature: 0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("jd: llm call: %w", err)
	}

	var out JD
	if err := json.Unmarshal([]byte(jsonx.Extract(resp.Content)), &out); err != nil {
		return nil, fmt.Errorf("jd: parse model JSON: %w\nraw output:\n%s", err, resp.Content)
	}

	out.normalize()
	if len(out.RequiredSkills) == 0 && len(out.Keywords) == 0 {
		return nil, fmt.Errorf("jd: no skills or keywords extracted")
	}

	a.log.Info("jd analyzed",
		"title", out.Title, "required_skills", len(out.RequiredSkills),
		"keywords", len(out.Keywords), "provider", resp.Provider)
	return &out, nil
}

// normalize trims whitespace and removes empty / duplicate terms (case-
// insensitively) from every list, so downstream matching isn't skewed by dupes.
func (j *JD) normalize() {
	j.Title = strings.TrimSpace(j.Title)
	j.Seniority = strings.ToLower(strings.TrimSpace(j.Seniority))
	j.RequiredSkills = cleanTerms(j.RequiredSkills)
	j.NiceToHave = cleanTerms(j.NiceToHave)
	j.Keywords = cleanTerms(j.Keywords)
	j.Responsibilities = cleanTerms(j.Responsibilities)
}

func cleanTerms(terms []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}

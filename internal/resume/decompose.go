package resume

import (
	"ai-resume-tailor/internal/jsonx"
	"ai-resume-tailor/internal/llm"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// Depend on this small interface, not the concrete *llm.Client,
type completer interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

type Decomposer struct {
	llm completer
	log *slog.Logger
}

func NewDecomposer(c completer, log *slog.Logger) *Decomposer {
	if log == nil {
		log = slog.Default()
	}
	return &Decomposer{llm: c, log: log}
}

// The "extract only what's present" rules are the extraction-time half
// of the no-fabrication guardrail.
const decomposeSystemPrompt = `You are a precise resume parser. Given the raw text of a resume, extract its content into a flat list of structured "items". An item is a single reusable unit: one role, one accomplishment bullet, one skill group, one education entry, or one project.

Rules:
- Extract ONLY what is present in the resume. Never invent, infer, embellish, or add metrics, technologies, or achievements that are not explicitly stated.
- Copy accomplishment text faithfully. You may fix obvious formatting artifacts, but do not change meaning or add detail.
- For "metrics", list only quantified results that literally appear in the text (e.g. "reduced latency by 40%", "99.99% uptime"). If none, use [].
- For "skills", list only technologies or tools explicitly mentioned for that item.
- "type" must be exactly one of: experience, achievement, skill, education, project.
- Return ONLY a JSON object. No prose, no explanation, no markdown code fences.

Schema:
{"items":[{"type":"...","title":"...","company":"...","content":"...","skills":["..."],"metrics":["..."],"start_date":"...","end_date":"..."}]}`

type decodeResult struct {
	Items []Item `json:"items"`
}

func (d *Decomposer) Decompose(ctx context.Context, resumeText string) ([]Item, error) {
	resumeText = strings.TrimSpace(resumeText)
	if resumeText == "" {
		return nil, fmt.Errorf("empty resume text")
	}

	resp, err := d.llm.Complete(ctx, llm.Request{
		System: decomposeSystemPrompt,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: resumeText},
		},
		MaxTokens:   4096,
		Temperature: 0.1, //we want faithful extraction, not creativity
	})
	if err != nil {
		return nil, fmt.Errorf("resume: llm call: %w", err)
	}

	jsonText := jsonx.Extract(resp.Content)

	var result decodeResult
	if err := json.Unmarshal([]byte(jsonText), &result); err != nil {
		return nil, fmt.Errorf("resume: parse model JSON: %w\nraw output:\n%s", err, resp.Content)
	}
	items := d.validate(result.Items)
	if len(items) == 0 {
		return nil, fmt.Errorf("resume: model returned no usuable items")
	}

	d.log.Info("resume decomposed",
		"items", len(items), "provider", resp.Provider, "model", resp.Model)
	return items, nil
}

func (d *Decomposer) validate(raw []Item) []Item {
	out := make([]Item, 0, len(raw))
	seen := make(map[string]bool)

	for i, item := range raw {
		item.Content = strings.TrimSpace(item.Content)
		item.Title = strings.TrimSpace(item.Title)
		item.Company = strings.TrimSpace(item.Company)

		if !item.Type.valid() {
			d.log.Warn("dropping item with invalid type", "index", i, "type", item.Type)
			continue
		}
		if item.Content == "" {
			d.log.Warn("dropping item with empty content", "index", i)
			continue
		}
		item.ID = makeID(item.Type, item.Content)
		if seen[item.ID] {
			continue //type + content = same item, remove duplication
		}
		seen[item.ID] = true
		out = append(out, item)
	}
	return out
}

func makeID(t ItemType, content string) string {
	sum := sha256.Sum256([]byte(string(t) + "|" + content))
	prefix := string(t)
	if len(prefix) > 3 {
		prefix = prefix[:3]
	}
	return prefix + "-" + hex.EncodeToString(sum[:])[:8]
}

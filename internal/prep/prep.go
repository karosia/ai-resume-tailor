// Package prep generates interview preparation: likely questions for a role,
// each with an example answer grounded in the candidate's verified resume items.
// Like the tailor package, it holds a hard line against fabrication — answers may
// only draw on the candidate's real experience, and a check flags any answer that
// cites an item the candidate doesn't have.
package prep

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ai-resume-tailor/internal/jd"
	"ai-resume-tailor/internal/jsonx"
	"ai-resume-tailor/internal/llm"
	"ai-resume-tailor/internal/resume"
)

// Category groups questions by kind so prep reads like a real interview loop.
type Category string

const (
	CategoryTechnical    Category = "technical"
	CategorySystemDesign Category = "system-design"
	CategoryRoleSpecific Category = "role-specific"
	CategoryBehavioral   Category = "behavioral"
)

// Question is one expected interview question plus a grounded example answer.
// ItemIDs records which resume items the answer draws on — the hook that lets
// us verify the answer against the candidate's real experience.
type Question struct {
	Category Category `json:"category"`
	Question string   `json:"question"`
	Answer   string   `json:"answer"`
	ItemIDs  []string `json:"item_ids"`
}

type Prep struct {
	Questions []Question `json:"questions"`
}

// Warning flags an answer that cites an item the candidate doesn't have — a
// possible invented experience the candidate should review before relying on it.
type Warning struct {
	Question string
	ItemID   string
	Reason   string
}

type Result struct {
	Prep     *Prep
	Warnings []Warning
}

type completer interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

type Generator struct {
	llm completer
	log *slog.Logger
}

func NewGenerator(c completer, log *slog.Logger) *Generator {
	if log == nil {
		log = slog.Default()
	}
	return &Generator{llm: c, log: log}
}

const prepSystemPrompt = `You are an interview coach. Given a candidate's VERIFIED resume items and a target job, produce a set of likely interview questions with example answers.

Rules:
- Generate a realistic mix for THIS role. Use "category" values exactly: technical, system-design, role-specific, behavioral.
- For each question, draft an example answer that draws ONLY on the candidate's provided items. Set "item_ids" to the ids of the items the answer draws on.
- Prefer concrete, STAR-style answers (situation, task, action, result) built from the candidate's real experience.
- NEVER invent experience, employers, projects, or metrics the candidate does not have. If the role needs a skill the candidate's items don't show, still include the question, but write the answer as an HONEST bridge from adjacent experience they DO have (relating a technology to a similar one they have used). Never claim experience they lack; in that case cite the adjacent items you drew on.
- Return ONLY a JSON object. No prose, no markdown code fences.

Schema:
{"questions":[{"category":"...","question":"...","answer":"...","item_ids":["..."]}]}`

// Generate produces interview prep grounded in the candidate's items, and
// verifies that every answer's cited items actually exist.
func (g *Generator) Generate(ctx context.Context, items []resume.Item, j *jd.JD) (*Result, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("prep: no items to ground answers in")
	}

	payload := struct {
		Job   *jd.JD        `json:"job"`
		Items []resume.Item `json:"items"`
	}{Job: j, Items: items}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("prep: marshal payload: %w", err)
	}

	resp, err := g.llm.Complete(ctx, llm.Request{
		System:      prepSystemPrompt,
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: "Job and verified items:\n" + string(raw)}},
		MaxTokens:   8192,
		Temperature: 0.4,
	})
	if err != nil {
		return nil, fmt.Errorf("prep: llm call: %w", err)
	}

	if resp.Truncated() {
		return nil, fmt.Errorf("prep: the model's response was cut off at the token limit — " +
			"raise MaxTokens in prep.go and try again")
	}

	var p Prep
	if err := json.Unmarshal([]byte(jsonx.Extract(resp.Content)), &p); err != nil {
		return nil, fmt.Errorf("prep: parse model JSON: %w\nraw output:\n%s", err, resp.Content)
	}
	if len(p.Questions) == 0 {
		return nil, fmt.Errorf("prep: model returned no questions")
	}

	warnings := verify(&p, items)
	g.log.Info("interview prep generated",
		"questions", len(p.Questions), "warnings", len(warnings), "provider", resp.Provider)
	return &Result{Prep: &p, Warnings: warnings}, nil
}

// verify flags answers that cite item ids the candidate doesn't have.
func verify(p *Prep, items []resume.Item) []Warning {
	known := make(map[string]bool, len(items))
	for _, it := range items {
		known[it.ID] = true
	}
	var ws []Warning
	for _, q := range p.Questions {
		for _, id := range q.ItemIDs {
			if !known[id] {
				ws = append(ws, Warning{
					Question: q.Question,
					ItemID:   id,
					Reason:   "answer cites an item you don't have (possible invented experience)",
				})
			}
		}
	}
	return ws
}

// Render turns interview prep into readable markdown, grouped by category.
func Render(p *Prep) string {
	var b strings.Builder
	order := []Category{CategoryTechnical, CategorySystemDesign, CategoryRoleSpecific, CategoryBehavioral}
	titles := map[Category]string{
		CategoryTechnical:    "Technical",
		CategorySystemDesign: "System Design",
		CategoryRoleSpecific: "Role-Specific",
		CategoryBehavioral:   "Behavioral",
	}

	shown := make(map[int]bool)
	for _, cat := range order {
		first := true
		for i, q := range p.Questions {
			if q.Category != cat {
				continue
			}
			if first {
				fmt.Fprintf(&b, "## %s\n\n", titles[cat])
				first = false
			}
			fmt.Fprintf(&b, "**Q: %s**\n\n%s\n\n", q.Question, q.Answer)
			shown[i] = true
		}
	}

	// Any questions with an unrecognized category still get shown.
	firstOther := true
	for i, q := range p.Questions {
		if shown[i] {
			continue
		}
		if firstOther {
			b.WriteString("## Other\n\n")
			firstOther = false
		}
		fmt.Fprintf(&b, "**Q: %s**\n\n%s\n\n", q.Question, q.Answer)
	}
	return b.String()
}

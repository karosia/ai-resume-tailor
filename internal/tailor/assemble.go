package tailor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"ai-resume-tailor/internal/jd"
	"ai-resume-tailor/internal/jsonx"
	"ai-resume-tailor/internal/llm"
	"ai-resume-tailor/internal/resume"
)

// Tailored is a JD-targeted resume assembled from verified items. Every bullet
// carries the ItemID it was derived from, which is what lets Verify check the
// bullet's facts against its source.
type Tailored struct {
	Summary  string    `json:"summary"`
	Sections []Section `json:"sections"`
}

type Section struct {
	Heading string   `json:"heading"`
	Bullets []Bullet `json:"bullets"`
}

type Bullet struct {
	ItemID string `json:"item_id"`
	Text   string `json:"text"`
}

// Violation records one place where the assembled resume broke the
// no-fabrication rule: a fact (currently: a number) that isn't in its source
// item, or a bullet that cites an item that doesn't exist.
type Violation struct {
	ItemID string
	Text   string
	Reason string
}

// Result bundles the assembled resume with any remaining verification
// violations, so the caller can surface them to the user.
type Result struct {
	Tailored   *Tailored
	Violations []Violation
}

type completerA interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Assembler turns verified items + a JD into a tailored resume, enforcing the
// no-fabrication guardrail with a post-generation check and one self-correction
// retry.
type Assembler struct {
	llm completerA
	log *slog.Logger
}

func NewAssembler(c completerA, log *slog.Logger) *Assembler {
	if log == nil {
		log = slog.Default()
	}
	return &Assembler{llm: c, log: log}
}

const assembleSystemPrompt = `You are a resume tailoring assistant. You are given a candidate's VERIFIED resume items and a target job's requirements. Produce a tailored resume that emphasizes the items most relevant to the job and echoes the job's terminology where it is truthful to do so.

HARD RULES (a downstream checker enforces these automatically):
- Use ONLY the provided items. Every bullet must set "item_id" to the id of the item it is based on.
- NEVER introduce a number, percentage, metric, or date that is not present in that source item. You may rephrase, reorder, and re-emphasize words, but you may not add, inflate, or change any figure.
- Do not invent items, employers, technologies, or achievements. If an item does not fit the job, simply omit it.
- Keep each bullet concise and results-oriented.
- Return ONLY a JSON object. No prose, no explanation, no markdown code fences.

Schema:
{"summary":"...","sections":[{"heading":"...","bullets":[{"item_id":"...","text":"..."}]}]}`

// Assemble generates a tailored resume, verifies it, and — if the first draft
// fabricates anything — feeds the specific violations back for one correction
// attempt. It returns the best draft plus whatever violations still remain.
func (a *Assembler) Assemble(ctx context.Context, items []resume.Item, j *jd.JD) (*Result, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("tailor: no items to assemble from")
	}

	userMsg, err := buildUserMessage(items, j)
	if err != nil {
		return nil, err
	}

	draft, err := a.generate(ctx, userMsg)
	if err != nil {
		return nil, err
	}

	violations := Verify(draft, items)
	if len(violations) == 0 {
		return &Result{Tailored: draft, Violations: nil}, nil
	}

	// Self-correction: tell the model exactly what it got wrong and try once more.
	a.log.Warn("tailored draft failed verification, retrying once",
		"violations", len(violations))

	fixMsg := userMsg + "\n\nYour previous attempt broke the rules in these places:\n" +
		formatViolations(violations) +
		"\nRegenerate the full JSON, removing or correcting every figure that is not present in the cited item."

	retry, err := a.generate(ctx, fixMsg)
	if err != nil {
		// Retry call failed; return the first draft with its violations surfaced.
		return &Result{Tailored: draft, Violations: violations}, nil
	}

	retryViolations := Verify(retry, items)
	if len(retryViolations) <= len(violations) {
		return &Result{Tailored: retry, Violations: retryViolations}, nil
	}
	return &Result{Tailored: draft, Violations: violations}, nil
}

// generate does one LLM round trip and parses the Tailored JSON.
func (a *Assembler) generate(ctx context.Context, userMsg string) (*Tailored, error) {
	resp, err := a.llm.Complete(ctx, llm.Request{
		System:      assembleSystemPrompt,
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: userMsg}},
		MaxTokens:   8192,
		Temperature: 0.3, // a little room to rephrase, still grounded
	})
	if err != nil {
		return nil, fmt.Errorf("tailor: llm call: %w", err)
	}

	if resp.Truncated() {
		return nil, fmt.Errorf("tailor: the model's response was cut off at the token limit — " +
			"you may have many items. Raise MaxTokens in assemble.go and try again")
	}

	var t Tailored
	if err := json.Unmarshal([]byte(jsonx.Extract(resp.Content)), &t); err != nil {
		return nil, fmt.Errorf("tailor: parse model JSON: %w\nraw output:\n%s", err, resp.Content)
	}
	return &t, nil
}

// buildUserMessage serializes the JD terms and the item pool for the model.
func buildUserMessage(items []resume.Item, j *jd.JD) (string, error) {
	payload := struct {
		Job   *jd.JD        `json:"job"`
		Items []resume.Item `json:"items"`
	}{Job: j, Items: items}

	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("tailor: marshal prompt payload: %w", err)
	}
	return "Job requirements and verified items:\n" + string(raw), nil
}

func formatViolations(vs []Violation) string {
	var b strings.Builder
	for _, v := range vs {
		fmt.Fprintf(&b, "- item %s: %s (in bullet: %q)\n", v.ItemID, v.Reason, v.Text)
	}
	return b.String()
}

// --- verification (deterministic, no LLM) ---

// numberPattern matches number-like tokens: a leading digit followed by any mix
// of digits, dots, and commas. Catches "40", "99.99", "2,000", "3.5".
var numberPattern = regexp.MustCompile(`[0-9][0-9.,]*`)

// numberSet extracts the normalized set of numbers appearing in s. Commas are
// stripped ("2,000" -> "2000") and trailing separators trimmed, so different
// spellings of the same figure compare equal.
func numberSet(s string) map[string]bool {
	out := make(map[string]bool)
	for _, m := range numberPattern.FindAllString(s, -1) {
		n := strings.ReplaceAll(m, ",", "")
		n = strings.TrimRight(n, ".")
		if n != "" {
			out[n] = true
		}
	}
	return out
}

// itemAllText gathers every string field of an item, so any figure the item
// legitimately contains (in content, metrics, dates, etc.) counts as a source.
func itemAllText(it resume.Item) string {
	parts := []string{it.Title, it.Company, it.Content, it.StartDate, it.EndDate}
	parts = append(parts, it.Skills...)
	parts = append(parts, it.Metrics...)
	return strings.Join(parts, " ")
}

// Verify checks a tailored resume against its source items and returns every
// fabrication it finds. It is deterministic and LLM-free — the enforceable half
// of the no-fabrication guardrail.
//
// What it catches: (1) bullets citing an item id that doesn't exist, and (2)
// any number in a bullet (or the summary) that isn't present in the source.
// What it can't catch: purely qualitative embellishment ("led" -> "spearheaded
// a company-wide"). That's left to the prompt and to the user's own review —
// numbers are simply the highest-risk, most detectable class of fabrication.
func Verify(t *Tailored, items []resume.Item) []Violation {
	known := make(map[string]bool, len(items))
	srcNums := make(map[string]map[string]bool, len(items))
	poolNums := make(map[string]bool)

	for _, it := range items {
		known[it.ID] = true
		nums := numberSet(itemAllText(it))
		srcNums[it.ID] = nums
		for n := range nums {
			poolNums[n] = true
		}
	}

	var vs []Violation

	// The summary has no single source item, so its numbers are checked against
	// the whole pool.
	for n := range numberSet(t.Summary) {
		if !poolNums[n] {
			vs = append(vs, Violation{
				ItemID: "(summary)",
				Text:   t.Summary,
				Reason: "number " + n + " not found in any source item",
			})
		}
	}

	for _, sec := range t.Sections {
		for _, b := range sec.Bullets {
			if !known[b.ItemID] {
				vs = append(vs, Violation{
					ItemID: b.ItemID,
					Text:   b.Text,
					Reason: "cites an unknown item id",
				})
				continue
			}
			for n := range numberSet(b.Text) {
				if !srcNums[b.ItemID][n] {
					vs = append(vs, Violation{
						ItemID: b.ItemID,
						Text:   b.Text,
						Reason: "number " + n + " is not in the source item",
					})
				}
			}
		}
	}
	return vs
}

// Render turns a tailored resume into readable markdown.
func Render(t *Tailored) string {
	var b strings.Builder
	if t.Summary != "" {
		b.WriteString(t.Summary)
		b.WriteString("\n\n")
	}
	for _, s := range t.Sections {
		fmt.Fprintf(&b, "## %s\n", s.Heading)
		for _, bl := range s.Bullets {
			fmt.Fprintf(&b, "- %s\n", bl.Text)
		}
		b.WriteString("\n")
	}
	return b.String()
}

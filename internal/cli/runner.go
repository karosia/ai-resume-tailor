package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"ai-resume-tailor/internal/jd"
	"ai-resume-tailor/internal/llm"
	"ai-resume-tailor/internal/prep"
	"ai-resume-tailor/internal/resume"
	"ai-resume-tailor/internal/tailor"
)

// llmRunner implements web.Runner. It performs the same steps the `tailor` and
// `prep` CLI commands do, but takes the JD as text (from a web form) and returns
// the rendered result as a string instead of writing files. Keeping this in the
// cli package lets the web package depend only on the small Runner interface.
type llmRunner struct {
	log *slog.Logger
}

func (r llmRunner) Tailor(ctx context.Context, jdText string) (string, error) {
	items, analyzed, client, err := r.setup(ctx, jdText)
	if err != nil {
		return "", err
	}
	res, err := tailor.NewAssembler(client, r.log).Assemble(ctx, items, analyzed)
	if err != nil {
		return "", fmt.Errorf("assemble: %w", err)
	}

	out := tailor.Render(res.Tailored, items, resumeHeader())
	if len(res.Violations) > 0 {
		out += "\n\n----------\nVERIFICATION — " + strconv.Itoa(len(res.Violations)) + " issue(s) need your review:\n"
		for _, v := range res.Violations {
			out += fmt.Sprintf("  - [%s] %s\n    in: %q\n", v.ItemID, v.Reason, v.Text)
		}
	} else {
		out += "\n\n----------\nVerification: PASSED — no fabricated figures detected."
	}
	return out, nil
}

func (r llmRunner) Prep(ctx context.Context, jdText string) (string, error) {
	items, analyzed, client, err := r.setup(ctx, jdText)
	if err != nil {
		return "", err
	}
	res, err := prep.NewGenerator(client, r.log).Generate(ctx, items, analyzed)
	if err != nil {
		return "", fmt.Errorf("generate prep: %w", err)
	}

	out := prep.Render(res.Prep)
	if len(res.Warnings) > 0 {
		out += "\n\n----------\nGROUNDING — " + strconv.Itoa(len(res.Warnings)) + " answer(s) need your review:\n"
		for _, wn := range res.Warnings {
			out += fmt.Sprintf("  - %s\n    (%s: %s)\n", wn.Question, wn.ItemID, wn.Reason)
		}
	} else {
		out += "\n\n----------\nGrounding: PASSED — every answer draws on items you actually have."
	}
	return out, nil
}

// setup is the shared front half: load stored items, build the client, and
// analyze the pasted JD text. Mirrors analyzeAgainstItems, but takes JD text
// rather than a file path.
func (r llmRunner) setup(ctx context.Context, jdText string) ([]resume.Item, *jd.JD, *llm.Client, error) {
	items, err := loadItems("items.json")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not load items.json — run `ai-resume-tailor decompose` first: %w", err)
	}
	client, err := buildClient(r.log)
	if err != nil {
		return nil, nil, nil, err
	}
	analyzed, err := jd.NewAnalyzer(client, r.log).Analyze(ctx, jdText)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("analyze jd: %w", err)
	}
	return items, analyzed, client, nil
}

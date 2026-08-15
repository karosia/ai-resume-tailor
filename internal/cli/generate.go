package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"ai-resume-tailor/internal/llm"
	"ai-resume-tailor/internal/prep"
	"ai-resume-tailor/internal/resume"
	"ai-resume-tailor/internal/tailor"
)

func runPing(log *slog.Logger) error {
	client, err := buildClient(log)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := client.Complete(ctx, llm.Request{
		System:      "You are a terse assistant. Answer in one sentence.",
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: "Say hello and name yourself."}},
		MaxTokens:   200,
		Temperature: 0.7,
	})
	if err != nil {
		return fmt.Errorf("completion failed: %w", err)
	}
	fmt.Printf("\n[answered by %s / %s]\n%s\n", resp.Provider, resp.Model, resp.Content)
	return nil
}

func runDecompose(log *slog.Logger, args []string) error {
	var path string
	if len(args) >= 1 {
		path = cleanPath(args[0])
	} else {
		path = cleanPath(promptLine("Enter path to your resume (.txt or .pdf): "))
		if path == "" {
			return usagef("no resume path provided")
		}
	}

	text, err := resume.ReadResumeFile(path)
	if err != nil {
		return fmt.Errorf("read resume %s: %w", path, err)
	}

	client, err := buildClient(log)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	items, err := resume.NewDecomposer(client, log).Decompose(ctx, text)
	if err != nil {
		return fmt.Errorf("decompose: %w", err)
	}

	out, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal items: %w", err)
	}
	const outPath = "items.json"
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		return fmt.Errorf("write items: %w", err)
	}

	fmt.Printf("\nExtracted %d items -> %s\n\n", len(items), outPath)
	for _, it := range items {
		fmt.Printf("  [%s] %-11s %s\n", it.ID, it.Type, summarize(it))
	}
	return nil
}

func runMatch(log *slog.Logger, args []string) error {
	jdArg, err := jdArgOrPrompt(args)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	items, analyzed, _, err := analyzeAgainstItems(ctx, log, jdArg)
	if err != nil {
		return err
	}

	res := tailor.Match(items, analyzed)

	fmt.Printf("\nJD: %s", analyzed.Title)
	if analyzed.Seniority != "" {
		fmt.Printf("  (%s)", analyzed.Seniority)
	}
	fmt.Printf("\nKeyword coverage: %d%%  (%d of %d terms)\n",
		res.CoveragePercent, len(res.CoveredTerms), len(res.CoveredTerms)+len(res.MissingTerms))

	if len(res.MissingTerms) > 0 {
		fmt.Printf("Missing terms: %s\n", strings.Join(res.MissingTerms, ", "))
	}

	fmt.Printf("\nTop matching items:\n")
	limit := len(res.Ranked)
	if limit > 10 {
		limit = 10
	}
	for _, si := range res.Ranked[:limit] {
		fmt.Printf("  [%s] score %d  covers: %s\n",
			si.Item.ID, si.Score, strings.Join(si.Matched, ", "))
	}
	fmt.Println()
	return nil
}
func runTailor(log *slog.Logger, args []string) error {
	jdArg, err := jdArgOrPrompt(args)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	items, analyzed, client, err := analyzeAgainstItems(ctx, log, jdArg)
	if err != nil {
		return err
	}

	res, err := tailor.NewAssembler(client, log).Assemble(ctx, items, analyzed)
	if err != nil {
		return fmt.Errorf("assemble: %w", err)
	}

	header := resumeHeader()

	// File names incorporate the company (when the JD names one) and today's
	// date, matching the canonical resume naming. Falls back to a generic name.
	base := resumeFileBase(header.Name, analyzed.Company)
	pdfPath := base + ".pdf"
	mdPath := base + ".md"

	pdfBytes, err := tailor.RenderPDF(res.Tailored, items, header)
	if err != nil {
		return fmt.Errorf("render pdf: %w", err)
	}
	if err := os.WriteFile(pdfPath, pdfBytes, 0o644); err != nil {
		return fmt.Errorf("write pdf: %w", err)
	}

	md := tailor.Render(res.Tailored, items, header)
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		return fmt.Errorf("write tailored resume: %w", err)
	}

	// Queue this as a draft application in the tracker, capturing the JD, so the
	// user can review and later advance it.
	if note := saveDraftApplication(log, analyzed, jdArg); note != "" {
		fmt.Println(note)
	}

	fmt.Printf("\nTailored resume for: %s -> %s, %s\n\n", analyzed.Title, mdPath, pdfPath)
	fmt.Println(md)

	if len(res.Violations) == 0 {
		fmt.Println("Verification: PASSED — no fabricated figures detected.")
	} else {
		fmt.Printf("Verification: %d issue(s) need your review:\n", len(res.Violations))
		for _, v := range res.Violations {
			fmt.Printf("  - [%s] %s\n    in: %q\n", v.ItemID, v.Reason, v.Text)
		}
		fmt.Println("\nThese figures could not be traced to your source items. Fix or remove them before using this resume.")
	}
	return nil
}

func runPrep(log *slog.Logger, args []string) error {
	jdArg, err := jdArgOrPrompt(args)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	items, analyzed, client, err := analyzeAgainstItems(ctx, log, jdArg)
	if err != nil {
		return err
	}

	res, err := prep.NewGenerator(client, log).Generate(ctx, items, analyzed)
	if err != nil {
		return fmt.Errorf("generate prep: %w", err)
	}

	md := prep.Render(res.Prep)
	const outPath = "interview_prep.md"
	if err := os.WriteFile(outPath, []byte(md), 0o644); err != nil {
		return fmt.Errorf("write prep: %w", err)
	}

	fmt.Printf("\nInterview prep for: %s -> %s\n\n", analyzed.Title, outPath)
	fmt.Println(md)

	if len(res.Warnings) == 0 {
		fmt.Println("Grounding: PASSED — every answer draws on items you actually have.")
	} else {
		fmt.Printf("Grounding: %d answer(s) need your review:\n", len(res.Warnings))
		for _, w := range res.Warnings {
			fmt.Printf("  - %s\n    (%s: %s)\n", w.Question, w.ItemID, w.Reason)
		}
		fmt.Println("\nThese answers reference experience not in your items. Rework or drop them before the interview.")
	}
	return nil
}

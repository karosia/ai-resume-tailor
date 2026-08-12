package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-resume-tailor/internal/jd"
	"ai-resume-tailor/internal/llm"
	"ai-resume-tailor/internal/resume"
	"ai-resume-tailor/internal/tailor"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "ping":
		runPing(log)
	case "decompose":
		runDecompose(log, os.Args[2:])
	case "match":
		runMatch(log, os.Args[2:])
	case "tailor":
		runTailor(log, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `jobtailor — resume tailoring & application tracking

Usage:
  jobtailor ping                         verify the LLM provider chain works
  jobtailor decompose [resume.txt|.pdf]  extract structured items (asks for a path if omitted)
  jobtailor match <jd.txt>               match stored items against a job description
  jobtailor tailor <jd.txt>              assemble a tailored resume (with fabrication check)
`)
}

// buildClient assembles the provider failover chain from environment variables.
// Order = priority: Anthropic first, OpenAI as fallback.
func buildClient(log *slog.Logger) (*llm.Client, error) {
	var providers []llm.Provider
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		providers = append(providers, llm.NewAnthropicProvider(key, os.Getenv("ANTHROPIC_MODEL")))
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		providers = append(providers, llm.NewOpenAIProvider(key, os.Getenv("OPENAI_MODEL")))
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("no API keys set; export ANTHROPIC_API_KEY and/or OPENAI_API_KEY")
	}
	return llm.NewClient(log, providers...)
}

func runPing(log *slog.Logger) {
	client, err := buildClient(log)
	if err != nil {
		log.Error("setup", "error", err)
		os.Exit(1)
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
		log.Error("completion failed", "error", err)
		os.Exit(1)
	}
	fmt.Printf("\n[answered by %s / %s]\n%s\n", resp.Provider, resp.Model, resp.Content)
}

func runDecompose(log *slog.Logger, args []string) {
	var path string
	if len(args) >= 1 {
		path = cleanPath(args[0])
	} else {
		// No path given: ask for one interactively.
		path = cleanPath(promptLine("Enter path to your resume (.txt or .pdf): "))
		if path == "" {
			log.Error("no resume path provided")
			os.Exit(2)
		}
	}

	text, err := resume.ReadResumeFile(path)
	if err != nil {
		log.Error("read resume", "path", path, "error", err)
		os.Exit(1)
	}

	client, err := buildClient(log)
	if err != nil {
		log.Error("setup", "error", err)
		os.Exit(1)
	}

	// This is the single source of truth for the overall request budget; it is
	// smaller than each provider's HTTP safety-net timeout, so it always wins.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	decomposer := resume.NewDecomposer(client, log)
	items, err := decomposer.Decompose(ctx, text)
	if err != nil {
		log.Error("decompose failed", "error", err)
		os.Exit(1)
	}

	// Persist to items.json. (M4 will replace this with a SQLite-backed store;
	// for now a plain JSON file keeps things inspectable.)
	out, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		log.Error("marshal items", "error", err)
		os.Exit(1)
	}
	const outPath = "items.json"
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		log.Error("write items", "error", err)
		os.Exit(1)
	}

	fmt.Printf("\nExtracted %d items -> %s\n\n", len(items), outPath)
	for _, it := range items {
		fmt.Printf("  [%s] %-11s %s\n", it.ID, it.Type, summarize(it))
	}
}

// summarize builds a one-line preview of an item for the terminal.
func summarize(it resume.Item) string {
	s := it.Content
	if it.Company != "" {
		s = it.Company + " — " + s
	}
	if len(s) > 70 {
		s = s[:67] + "..."
	}
	return s
}

// promptLine prints a prompt and reads one line from stdin.
func promptLine(prompt string) string {
	fmt.Print(prompt)
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		return sc.Text()
	}
	return ""
}

// cleanPath tidies a user-supplied path: trims spaces, strips surrounding quotes
// (terminals add these when you drag-and-drop a file), and expands a leading ~/.
func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, `"'`)
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	return p
}

// loadItems reads the items.json produced by `decompose`.
func loadItems(path string) ([]resume.Item, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []resume.Item
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return items, nil
}

func runMatch(log *slog.Logger, args []string) {
	if len(args) < 1 {
		log.Error("usage: jobtailor match <jd.txt>")
		os.Exit(2)
	}
	jdPath := args[0]

	items, err := loadItems("items.json")
	if err != nil {
		log.Error("could not load items.json — run `jobtailor decompose <resume.txt>` first", "error", err)
		os.Exit(1)
	}

	jdData, err := os.ReadFile(jdPath)
	if err != nil {
		log.Error("read jd", "path", jdPath, "error", err)
		os.Exit(1)
	}

	client, err := buildClient(log)
	if err != nil {
		log.Error("setup", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	analyzed, err := jd.NewAnalyzer(client, log).Analyze(ctx, string(jdData))
	if err != nil {
		log.Error("analyze jd", "error", err)
		os.Exit(1)
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
}

func runTailor(log *slog.Logger, args []string) {
	if len(args) < 1 {
		log.Error("usage: jobtailor tailor <jd.txt>")
		os.Exit(2)
	}
	jdPath := args[0]

	items, err := loadItems("items.json")
	if err != nil {
		log.Error("could not load items.json — run `jobtailor decompose <resume.txt>` first", "error", err)
		os.Exit(1)
	}

	jdData, err := os.ReadFile(jdPath)
	if err != nil {
		log.Error("read jd", "path", jdPath, "error", err)
		os.Exit(1)
	}

	client, err := buildClient(log)
	if err != nil {
		log.Error("setup", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	analyzed, err := jd.NewAnalyzer(client, log).Analyze(ctx, string(jdData))
	if err != nil {
		log.Error("analyze jd", "error", err)
		os.Exit(1)
	}

	res, err := tailor.NewAssembler(client, log).Assemble(ctx, items, analyzed)
	if err != nil {
		log.Error("assemble", "error", err)
		os.Exit(1)
	}

	// Also render a PDF. Name/contact come from the environment, never the
	// model — they're personal facts that live outside the resume items.
	pdfBytes, err := tailor.RenderPDF(res.Tailored, tailor.PDFOptions{
		Name:    os.Getenv("RESUME_NAME"),
		Contact: os.Getenv("RESUME_CONTACT"),
	})
	if err != nil {
		log.Error("render pdf", "error", err)
		os.Exit(1)
	}
	const pdfPath = "tailored.pdf"
	if err := os.WriteFile(pdfPath, pdfBytes, 0o644); err != nil {
		log.Error("write pdf", "error", err)
		os.Exit(1)
	}

	md := tailor.Render(res.Tailored)
	const outPath = "tailored.md"
	if err := os.WriteFile(outPath, []byte(md), 0o644); err != nil {
		log.Error("write tailored resume", "error", err)
		os.Exit(1)
	}

	fmt.Printf("\nTailored resume for: %s -> %s, %s\n\n", analyzed.Title, outPath, pdfPath)
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
}

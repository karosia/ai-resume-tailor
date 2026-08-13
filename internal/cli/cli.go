// Package cli implements the ai-resume-tailor command-line interface. main.go is a thin
// wrapper around Run; all dispatch and command handlers live here so the entry
// point stays tiny and the commands are testable as a package.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"ai-resume-tailor/internal/jd"
	"ai-resume-tailor/internal/llm"
	"ai-resume-tailor/internal/resume"
	"ai-resume-tailor/internal/store"
)

// UsageError signals a bad invocation (missing or malformed arguments). main
// maps it to exit code 2, distinct from other failures (exit code 1).
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

func usagef(format string, a ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, a...)}
}

// Run dispatches one command. It returns an error instead of exiting, so main
// controls the process exit code and this package stays testable.
func Run(args []string) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if len(args) < 1 {
		return &UsageError{Msg: usageText()}
	}

	switch args[0] {
	case "ping":
		return runPing(log)
	case "decompose":
		return runDecompose(log, args[1:])
	case "match":
		return runMatch(log, args[1:])
	case "tailor":
		return runTailor(log, args[1:])
	case "track":
		return runTrack(args[1:])
	case "apps":
		return runApps()
	case "status":
		return runStatus(args[1:])
	case "note":
		return runNote(args[1:])
	case "prep":
		return runPrep(log, args[1:])
	case "serve":
		return runServe(log, args[1:])
	default:
		return &UsageError{Msg: "unknown command: " + args[0] + "\n\n" + usageText()}
	}
}

func usageText() string {
	return `ai-resume-tailor — resume tailoring & application tracking

Usage:
  ai-resume-tailor ping                         verify the LLM provider chain works
  ai-resume-tailor decompose [resume.txt|.pdf]  extract structured items (asks for a path if omitted)
  ai-resume-tailor match <jd.txt>               match stored items against a job description
  ai-resume-tailor tailor <jd.txt>              assemble a tailored resume (with fabrication check)
  ai-resume-tailor track "<company>" "<role>"   start tracking a new application
  ai-resume-tailor apps                         list all tracked applications
  ai-resume-tailor status <id> <status>         update an application's status
  ai-resume-tailor note <id> <text...>          set an application's notes`
}

// --- shared helpers ---

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

func openStore() (*store.Store, error) {
	return store.Open("ai-resume-tailor.db")
}

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

// analyzeAgainstItems performs the setup shared by `match` and `tailor`: load
// the stored items, read the JD file, build the LLM client, and analyze the JD.
func analyzeAgainstItems(ctx context.Context, log *slog.Logger, jdPath string) ([]resume.Item, *jd.JD, *llm.Client, error) {
	items, err := loadItems("items.json")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not load items.json — run `ai-resume-tailor decompose` first: %w", err)
	}
	jdData, err := os.ReadFile(jdPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read jd %s: %w", jdPath, err)
	}
	client, err := buildClient(log)
	if err != nil {
		return nil, nil, nil, err
	}
	analyzed, err := jd.NewAnalyzer(client, log).Analyze(ctx, string(jdData))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("analyze jd: %w", err)
	}
	return items, analyzed, client, nil
}

func promptLine(prompt string) string {
	fmt.Print(prompt)
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		return sc.Text()
	}
	return ""
}

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

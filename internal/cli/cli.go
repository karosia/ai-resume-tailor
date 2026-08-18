// Package cli implements the ai-resume-tailor command-line interface. main.go is a thin
// wrapper around Run; all dispatch and command handlers live here so the entry
// point stays tiny and the commands are testable as a package.
package cli

import (
	"ai-resume-tailor/internal/jobsource"
	"ai-resume-tailor/internal/tailor"
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
	"ai-resume-tailor/internal/store"

	aitracecause "github.com/karosia/ai-trace-cause"
	oteltelemetry "github.com/karosia/ai-trace-cause/telemetry/otel"
	"go.opentelemetry.io/otel"
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

	shutdown := setupOTel(log)
	defer shutdown(context.Background())

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
  ai-resume-tailor prep <jd.txt>                generate interview prep grounded in your items

  ai-resume-tailor track "<company>" "<role>"   start tracking a new application
  ai-resume-tailor apps                         list all tracked applications
  ai-resume-tailor status <id> <status>         update an application's status
  ai-resume-tailor note <id> <text...>          set an application's notes
  ai-resume-tailor serve [addr]                 launch the tracking dashboard (default 127.0.0.1:8080)`
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

// analyzeAgainstItems performs the setup shared by `match`, `tailor`, and
// `prep`: load the stored items, build the client, resolve the JD (from a URL,
// a file, or raw text), and analyze it.
func analyzeAgainstItems(ctx context.Context, log *slog.Logger, jdArg string) ([]resume.Item, *jd.JD, *llm.Client, error) {
	items, err := loadItems("items.json")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not load items.json — run `ai-resume-tailor decompose` first: %w", err)
	}
	client, err := buildClient(log)
	if err != nil {
		return nil, nil, nil, err
	}

	// jdArg may be a URL, a file path, or the JD text itself.
	jdText, err := jobsource.NewResolver(client, log).Resolve(ctx, jdArg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve jd: %w", err)
	}

	analyzed, err := jd.NewAnalyzer(client, log).Analyze(ctx, jdText)
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

// promptJD reads a multi-line job description pasted into the terminal. A JD
// spans many paragraphs, so it can't end on the first newline; instead input
// ends on two consecutive blank lines (press Enter on an empty line twice).
// Single blank lines inside the JD are preserved as paragraph breaks. We use a
// bufio.Reader (not Scanner) so a very long single line can't overflow the
// scanner's token limit.
func promptJD() (string, error) {
	fmt.Println("Paste the job description below.")
	fmt.Println("When you're done, leave a blank line (press Enter twice) to finish:")
	fmt.Println("----------------------------------------------------------------")

	reader := bufio.NewReader(os.Stdin)
	var lines []string
	blanks := 0
	for {
		line, err := reader.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")

		if trimmed == "" {
			blanks++
		} else {
			blanks = 0
		}
		lines = append(lines, trimmed)

		if blanks >= 2 || err != nil { // two blank lines, or EOF/piped input
			break
		}
	}

	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if text == "" {
		return "", usagef("no job description was provided")
	}
	return text, nil
}

// jdArgOrPrompt returns the JD source: the CLI argument if given (a URL, file,
// or text), or — when omitted — prompts the user to paste the JD directly. This
// lets `match`/`tailor`/`prep` run with no argument at all.
func jdArgOrPrompt(args []string) (string, error) {
	if len(args) >= 1 && strings.TrimSpace(args[0]) != "" {
		return args[0], nil
	}
	return promptJD()
}

// resumeHeader loads the candidate's contact facts from the profile stored in
// the database (set via the web dashboard's Profile tab). These are facts about
// the person that live outside the tailored items, so the model never sees or
// invents them. A missing/empty profile yields an empty header, which the
// renderers handle gracefully.
func resumeHeader() tailor.Header {
	st, err := openStore()
	if err != nil {
		return tailor.Header{}
	}
	defer st.Close()

	p, err := st.GetProfile()
	if err != nil {
		return tailor.Header{}
	}
	return tailor.Header{
		Name:     p.Name,
		Email:    p.Email,
		Phone:    p.Phone,
		Location: p.Location,
		LinkedIn: p.LinkedIn,
		GitHub:   p.GitHub,
	}
}

// saveDraftApplication queues a tailored resume as a draft application in the
// tracker, capturing the JD. When the JD didn't name a company, it falls back to
// the role title (or "Untitled role") so the entry still appears and the user
// can rename it on the dashboard. Best-effort: returns a short status note for
// display, and never fails the caller.
func saveDraftApplication(log *slog.Logger, analyzed *jd.JD, jdText string) string {
	company := strings.TrimSpace(analyzed.Company)
	if company == "" {
		if t := strings.TrimSpace(analyzed.Title); t != "" {
			company = t // fallback so the draft is still created (rename later)
		} else {
			company = "Untitled role"
		}
	}

	st, err := openStore()
	if err != nil {
		log.Warn("could not open store to track draft", "error", err)
		return ""
	}
	defer st.Close()

	if _, err := st.AddDraft(company, analyzed.Title, jdText, analyzed.Title); err != nil {
		log.Warn("could not save draft application", "error", err)
		return ""
	}
	return fmt.Sprintf("Tracked as a draft application for %s.", company)
}

// resumeFileBase builds the output file base name (without extension) from the
// candidate name, the company (when known), and today's date — e.g.
// "Jayce_Park_Stripe_260814". Missing pieces are skipped; if nothing usable is
// present it falls back to "tailored".
func resumeFileBase(name, company string) string {
	var parts []string
	if s := fileToken(name); s != "" {
		parts = append(parts, s)
	}
	if s := fileToken(company); s != "" {
		parts = append(parts, s)
	}
	date := time.Now().Format("060102") // YYMMDD

	if len(parts) == 0 {
		return "tailored_" + date
	}
	parts = append(parts, date)
	return strings.Join(parts, "_")
}

// fileToken turns free text into a safe file-name token: keep letters and
// digits, turn runs of anything else into single underscores, trim underscores.
func fileToken(s string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

// explainTailorCausality builds a causal graph for a finished tailor run and
// returns human-readable explanations for the actions the caller cares about.
// It's best-effort: tracing is an observability aid, so a failure here logs and
// returns empty rather than failing the tailor command. The graph is in-memory
// and lives only for this call.
//
// When OpenTelemetry is enabled, the whole recording runs inside a span, and the
// ai-trace-cause otel hook stamps every recorded entity with that span's trace
// and span IDs — correlating "why" (the causal graph) with "how" (the OTel trace).
func explainTailorCausality(
	ctx context.Context,
	log *slog.Logger,
	t *tailor.Tailored,
	items []resume.Item,
	analyzed *jd.JD,
) []string {
	// Wrap the recording in an OpenTelemetry span so the causal entities can be
	// correlated with it. When OTel is disabled this span is a cheap no-op.
	ctx, span := otel.Tracer("ai-resume-tailor").Start(ctx, "tailor.causal_trace")
	defer span.End()

	tr, err := aitracecause.New(
		aitracecause.WithMemoryStore(),
		aitracecause.WithTelemetryHook(oteltelemetry.New()),
	)
	if err != nil {
		log.Warn("could not start causal trace", "error", err)
		return nil
	}
	res, err := tailor.RecordCausalTrace(ctx, tr, t, items, analyzed)
	if err != nil {
		log.Warn("could not record causal trace", "error", err)
		return nil
	}

	// Explain the flagged actions first — those are the ones a user most needs
	// the "why" for.
	var out []string
	for itemID, actionIDs := range res.Violations {
		for _, aid := range actionIDs {
			if s, err := tr.Explain(ctx, aid, 8); err == nil {
				out = append(out, fmt.Sprintf("[%s] %s", itemID, s))
			}
		}
	}
	return out
}

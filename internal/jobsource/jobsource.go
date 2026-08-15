// Package jobsource resolves a job description from wherever it lives — a URL, a
// local file, or text pasted directly — into clean JD text ready for analysis.
//
// For URLs it fetches the page and asks the LLM to pull out just the job
// description, discarding navigation, ads, and boilerplate. This works well on
// static pages; sites that render their content with JavaScript (LinkedIn and
// many large ATSes) return a near-empty shell, so the fetch falls back to a
// clear message telling the user to paste the JD as text instead.
package jobsource

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"ai-resume-tailor/internal/jsonx"
	"ai-resume-tailor/internal/llm"
)

type completer interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Resolver turns a URL / file path / raw text into JD text.
type Resolver struct {
	llm  completer
	log  *slog.Logger
	http *http.Client
}

func NewResolver(c completer, log *slog.Logger) *Resolver {
	if log == nil {
		log = slog.Default()
	}
	return &Resolver{
		llm:  c,
		log:  log,
		http: &http.Client{Timeout: 20 * time.Second},
	}
}

// Resolve inspects arg and returns JD text. arg may be an http(s) URL, a path to
// a .txt file, or (when it's neither) is treated as the JD text itself.
func (r *Resolver) Resolve(ctx context.Context, arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	switch {
	case strings.HasPrefix(arg, "http://"), strings.HasPrefix(arg, "https://"):
		return r.fromURL(ctx, arg)
	default:
		// A path to a readable file? Read it. Otherwise treat arg as raw text.
		if data, err := os.ReadFile(arg); err == nil {
			return strings.TrimSpace(string(data)), nil
		}
		return arg, nil
	}
}

// minHTMLTextChars: below this, the page almost certainly rendered its content
// with JavaScript (we only got a shell), so extraction can't succeed.
const minHTMLTextChars = 200

func (r *Resolver) fromURL(ctx context.Context, url string) (string, error) {
	raw, err := r.fetch(ctx, url)
	if err != nil {
		return "", err
	}

	stripped := stripHTML(raw)
	if len(stripped) < minHTMLTextChars {
		return "", fmt.Errorf("that page has little extractable text — it likely loads the job " +
			"description with JavaScript (common on LinkedIn and large job boards). " +
			"Copy the description into a .txt file, or paste it directly, and try that instead")
	}

	// Hand the visible text to the LLM to isolate just the job description.
	jd, err := r.extractJD(ctx, stripped)
	if err != nil {
		return "", err
	}
	if len(strings.TrimSpace(jd)) < minHTMLTextChars/2 {
		return "", fmt.Errorf("couldn't isolate a job description from that page; paste the JD text instead")
	}
	r.log.Info("resolved jd from url", "url", url, "chars", len(jd))
	return jd, nil
}

func (r *Resolver) fetch(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	// A normal-looking UA avoids the most trivial bot filters; we don't try to
	// defeat real anti-bot systems.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ai-resume-tailor/1.0)")

	resp, err := r.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch %s: server returned %s", url, resp.Status)
	}

	// Cap the read so a huge page can't blow up memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 3<<20)) // 3 MB
	if err != nil {
		return "", fmt.Errorf("read %s: %w", url, err)
	}
	return string(body), nil
}

// Go's regexp engine (RE2) has no backreferences, so we can't match a tag
// against its own closing tag with \1. Instead we list the non-content elements
// explicitly, one pattern each.
var (
	reComments   = regexp.MustCompile(`(?s)<!--.*?-->`)
	reTags       = regexp.MustCompile(`(?s)<[^>]+>`)
	reWhitespace = regexp.MustCompile(`[ \t\r\f\v]+`)
	reBlankLines = regexp.MustCompile(`\n\s*\n\s*\n+`)

	dropElements = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`),
		regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`),
		regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript>`),
		regexp.MustCompile(`(?is)<svg[^>]*>.*?</svg>`),
		regexp.MustCompile(`(?is)<head[^>]*>.*?</head>`),
	}
)

// stripHTML reduces an HTML document to its visible text. It's deliberately
// simple (stdlib regexp, no parser dependency): drop scripts/styles/comments,
// remove tags, collapse whitespace. The LLM cleans up the rest.
func stripHTML(s string) string {
	for _, re := range dropElements {
		s = re.ReplaceAllString(s, " ")
	}
	s = reComments.ReplaceAllString(s, " ")
	s = reTags.ReplaceAllString(s, " ")
	s = htmlUnescape(s)
	s = reWhitespace.ReplaceAllString(s, " ")
	// tidy line breaks
	s = strings.ReplaceAll(s, " \n", "\n")
	s = reBlankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// htmlUnescape handles the few entities common in JD text without pulling in a
// dependency.
func htmlUnescape(s string) string {
	repl := strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`,
		"&#39;", "'", "&apos;", "'", "&nbsp;", " ", "&mdash;", "—", "&ndash;", "–",
	)
	return repl.Replace(s)
}

const extractSystemPrompt = `You are given the visible text of a web page that contains a job posting, mixed with navigation, headers, footers, and other boilerplate. Extract ONLY the job description itself: the role summary, responsibilities, requirements, qualifications, and (if present) the tech stack.

Rules:
- Return the job description as clean plain text, preserving its meaningful structure (headings, bullet lists).
- Do NOT include site navigation, cookie notices, "apply now" widgets, related jobs, or company marketing unrelated to the role.
- Do NOT summarize, add, or invent anything. Copy the posting's wording.
- If the page does not actually contain a job description, return exactly: NO_JD_FOUND

Return ONLY a JSON object: {"jd":"...the extracted text..."}`

func (r *Resolver) extractJD(ctx context.Context, pageText string) (string, error) {
	// Keep the prompt within a sane size; JD pages are rarely huge after stripping.
	if len(pageText) > 24000 {
		pageText = pageText[:24000]
	}

	resp, err := r.llm.Complete(ctx, llm.Request{
		System:    extractSystemPrompt,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: pageText}},
		MaxTokens: 4096,
	})
	if err != nil {
		return "", fmt.Errorf("extract jd: llm call: %w", err)
	}

	var out struct {
		JD string `json:"jd"`
	}
	if err := jsonx.ExtractInto(resp.Content, &out); err != nil {
		return "", fmt.Errorf("extract jd: parse model output: %w", err)
	}
	if strings.Contains(out.JD, "NO_JD_FOUND") || strings.TrimSpace(out.JD) == "" {
		return "", fmt.Errorf("no job description found on that page; paste the JD text instead")
	}
	return strings.TrimSpace(out.JD), nil
}

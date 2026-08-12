# ai-resume-tailor

A resume tailoring and job application tool, written in Go.

Give it your resume and a job description. It breaks your resume into verified,
reusable items, matches them against the job, and assembles a tailored resume —
recombining and re-emphasizing facts you've actually recorded, and refusing to
invent figures you didn't. A deterministic checker enforces that last part.

> **Status: in development.** The core pipeline (resume → items → match →
> tailored resume with a fabrication check, output as Markdown and PDF) works
> end to end. Application tracking and interview prep are on the [roadmap](#roadmap).

## Why this exists

General-purpose resume tools don't know your actual accomplishments or your
standards, so auto-generated bullets tend to inflate or invent. ai-resume-tailor is
built around one hard constraint: the tailored resume may only be built from
facts already present in your stored items. Rewording and reordering are
allowed; inventing or inflating a number is not — and that rule is enforced by
code, not just by asking the model nicely.

## What it does today

- **Provider-agnostic LLM client with failover.** One `Provider` interface;
  ships with Anthropic (primary) and OpenAI (fallback). Add a backend by
  implementing two methods.
- **Resume decomposition.** Reads a `.txt` or `.pdf` resume and extracts it into
  structured, reusable items (roles, achievements, skills, education, projects)
  via structured LLM output. Content-addressed IDs make re-runs idempotent.
- **JD analysis + matching.** Analyzes a job description into required skills and
  keywords, then scores your items against it deterministically (no LLM, no
  fabrication risk), reporting honest keyword coverage and which terms are missing.
- **Guardrailed tailoring.** Assembles a tailored resume from your items, then a
  deterministic verifier checks every figure in the output against its source
  item. If a number can't be traced to a source, it's flagged; the model gets one
  self-correction attempt with the specific violations before anything is shown.
- **Markdown + PDF output.** Renders a clean, single-column, ATS-friendly PDF
  alongside a Markdown version.

### An honest note on the "coverage" number and the checker

The keyword coverage percentage is *literal* keyword coverage — the share of the
job's terms your items cover. It is **not** a prediction of any real ATS system's
score; those are proprietary and unknowable. Likewise, the verifier catches the
highest-risk, most detectable class of fabrication — **numbers and figures that
aren't in the source** — but it does not catch purely qualitative embellishment.
That's left to the prompt and to your own review.

## Requirements

- Go 1.24 or newer (matches the `go` directive in `go.mod`).
- At least one API key: `ANTHROPIC_API_KEY` and/or `OPENAI_API_KEY`.

> Note: the latest version of `github.com/ledongthuc/pdf` requires Go 1.24+. This
> project pins an earlier, Go 1.22-compatible version. Bump it if you're on a
> newer toolchain.

## Getting started

```bash
git clone https://github.com/karosia/ai-resume-tailor.git
cd ai-resume-tailor
go mod tidy

# Run the test suite — no API keys required.
go test ./...

# Configure providers and (optionally) your PDF header.
export ANTHROPIC_API_KEY=sk-ant-...
export OPENAI_API_KEY=sk-...                      # optional; enables fallback
export RESUME_NAME="Your Name"
export RESUME_CONTACT="City · you@example.com · github.com/you"
```

## Usage

```bash
# 1. Break your resume into reusable items -> items.json
#    Accepts .txt or .pdf; if you omit the path, it asks for one.
go run . decompose ~/Documents/resume.pdf

# 2. See how your items line up against a job posting.
go run . match jd.txt

# 3. Assemble a tailored resume -> tailored.md and tailored.pdf,
#    with a fabrication check reported at the end.
go run . tailor jd.txt
```

| Command                          | What it does                                                        |
| -------------------------------- | ------------------------------------------------------------------- |
| `ping`                           | Verify the LLM provider chain works                                 |
| `decompose [resume.txt\|.pdf]`   | Extract structured items (prompts for a path if omitted)            |
| `match <jd.txt>`                 | Match stored items against a job description, report coverage       |
| `tailor <jd.txt>`                | Assemble a tailored resume (Markdown + PDF) with a fabrication check |

### Configuration

| Variable            | Required | Default            | Purpose                            |
| ------------------- | -------- | ------------------ | ---------------------------------- |
| `ANTHROPIC_API_KEY` | yes\*    | —                  | Enables the Anthropic provider     |
| `ANTHROPIC_MODEL`   | no       | `claude-sonnet-5`  | Override the Anthropic model       |
| `OPENAI_API_KEY`    | no       | —                  | Enables the OpenAI fallback        |
| `OPENAI_MODEL`      | no       | `gpt-4o`           | Override the OpenAI model           |
| `RESUME_NAME`       | no       | —                  | Name printed in the PDF header      |
| `RESUME_CONTACT`    | no       | —                  | Contact line printed in the PDF     |

\* At least one provider key must be set. Primary/fallback order follows
registration order in `main.go` (Anthropic first, OpenAI second).

## Architecture

Nothing outside `internal/llm` imports a specific provider — the rest of the app
depends only on the `Provider` interface. LLM steps produce structured JSON;
matching and verification are deterministic and LLM-free, so the guardrail can't
itself hallucinate.
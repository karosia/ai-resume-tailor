# ai-resume-tailor

A resume tailoring, application-tracking, and interview-prep tool, written in Go.

Give it your resume and a job description. It breaks your resume into verified,
reusable items, matches them against the job, assembles a tailored resume, tracks
your applications, and drafts interview prep — recombining and re-emphasizing
facts you've actually recorded, and refusing to invent the ones you didn't.
Deterministic checkers enforce that last part.

> **Status: core pipeline complete.** Resume → items → match → tailored resume
> (Markdown + PDF) → application tracking → interview prep all work end to end.
> A web UI is the remaining item on the [roadmap](#roadmap).

## Why this exists

General-purpose resume tools don't know your actual accomplishments or your
standards, so auto-generated bullets tend to inflate or invent. ai-resume-tailor is
built around one hard constraint: tailored resumes and interview answers may only
be built from facts already present in your stored items. Rewording and
reordering are allowed; inventing or inflating a fact is not — and that rule is
enforced by code, not just by asking the model nicely.

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
  Output as clean, ATS-friendly PDF alongside Markdown.
- **Application tracking.** A local SQLite database records applications, their
  status, dates, and notes. The first time an application is marked "applied", the
  date is stamped automatically.
- **Interview prep.** Generates likely questions for the role with example answers
  grounded in your real experience. Each answer cites the items it draws on; a
  check flags any answer that references experience you don't have. When the role
  needs a skill your items don't show, answers bridge honestly from adjacent
  experience rather than claiming something you lack.

### An honest note on the "coverage" number and the checkers

The keyword coverage percentage is *literal* keyword coverage — the share of the
job's terms your items cover. It is **not** a prediction of any real ATS system's
score; those are proprietary and unknowable. The verifiers catch the highest-risk,
most detectable classes of fabrication — **figures not in the source** (tailoring)
and **answers citing experience you don't have** (interview prep) — but they don't
catch purely qualitative embellishment. That's left to the prompt and to your own
review.

## Requirements

- Go 1.26 or newer (matches the `go` directive in `go.mod`).
- At least one API key: `ANTHROPIC_API_KEY` and/or `OPENAI_API_KEY`.

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

# 3. Assemble a tailored resume -> tailored.md and tailored.pdf.
go run . tailor jd.txt

# 4. Generate interview prep grounded in your experience -> interview_prep.md
go run . prep jd.txt

# 5. Track the application and update it as things progress.
go run . track "Company" "Senior Backend Engineer"
go run . apps
go run . status 1 applied
go run . note 1 "referred by a friend"
```

| Command                          | What it does                                                        |
| -------------------------------- | ------------------------------------------------------------------- |
| `ping`                           | Verify the LLM provider chain works                                 |
| `decompose [resume.txt\|.pdf]`   | Extract structured items (prompts for a path if omitted)            |
| `match <jd.txt>`                 | Match stored items against a job description, report coverage       |
| `tailor <jd.txt>`                | Assemble a tailored resume (Markdown + PDF) with a fabrication check |
| `prep <jd.txt>`                  | Generate interview prep with answers grounded in your items         |
| `track "<company>" "<role>"`     | Start tracking a new application                                    |
| `apps`                           | List all tracked applications                                       |
| `status <id> <status>`           | Update an application's status                                      |
| `note <id> <text...>`            | Set an application's notes                                          |

Statuses: `draft`, `applied`, `interviewing`, `offer`, `accepted`, `rejected`, `withdrawn`.

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
registration order (Anthropic first, OpenAI second).

## Architecture

`main.go` is a thin entry point that calls `cli.Run` and maps errors to exit
codes. Everything else lives in focused `internal` packages. Nothing outside
`internal/llm` imports a specific provider — the rest of the app depends only on
the `Provider` interface. LLM steps produce structured JSON; matching and
verification are deterministic and LLM-free, so the guardrails can't themselves
hallucinate.
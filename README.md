# ai-resume-tailor

A local, single-binary tool that tailors your résumé to a specific job
description and tracks your applications — with a CLI and a small web dashboard.
It runs an LLM to rewrite your existing experience toward a posting, but never
invents facts: every tailored bullet is verified against your source material,
and contact details, company grouping, and dates come from your data, not the
model. It can also record *why* each line ended up in the résumé as a causal
graph, so "why is this here?" has a traceable answer.

## Why

Tailoring a résumé for each application is slow, and it's easy to drift into
claims you can't defend in an interview. This tool keeps the rewriting grounded
in what you've actually done — you provide the facts once, and every tailored
version is checked against them — while keeping your whole application pipeline
in one place.

## What it does

- **Decompose** your résumé into structured, reusable items (experience,
  achievements, skills, education, projects).
- **Analyze** a job description — pasted text, a local file, or a URL — into
  required skills, nice-to-haves, ATS keywords, responsibilities, and the
  hiring company.
- **Tailor** your résumé to that posting: it selects and rephrases your real
  bullets, groups them by company with a contact header, and exports Markdown
  and PDF.
- **Verify** that no figure or claim in the output is fabricated. Anything that
  can't be traced to a source item is flagged for your review.
- **Explain** why each flagged bullet was recorded as unverifiable, following a
  causal chain from the résumé item through the JD match to the decision and the
  action taken.
- **Prep** interview talking points grounded in items you actually have.
- **Track** applications through a pipeline (draft → applied → interviewing →
  offer → accepted), review the saved JD for each, and delete ones you don't
  need.

Tailoring a résumé also captures the job description and files the role as a
**draft** application automatically, so the tracker stays in sync with your work.

## Requirements

- Go 1.22 or newer
- An API key for at least one provider (Anthropic, OpenAI, and/or Gemini)

Everything else is pure Go — the PDF renderer, the SQLite driver, and the web
server have no system dependencies, so `go build` produces one self-contained
binary.

## Install

```bash
git clone https://github.com/karosia/ai-resume-tailor.git
cd ai-resume-tailor
go build -o ai-resume-tailor .
```

## Configuration

Set whichever provider keys you have. If more than one is present, the client
fails over from the primary to the next on error.

```bash
export ANTHROPIC_API_KEY=sk-ant-...
export OPENAI_API_KEY=sk-...            # optional fallback
export GEMINI_API_KEY=...               # optional fallback

# optional model overrides
export ANTHROPIC_MODEL=claude-sonnet-5
export OPENAI_MODEL=gpt-5.6-terra
export GEMINI_MODEL=gemini-2.5-flash

# optional: print OpenTelemetry spans for tailor runs to stdout
export OTEL_ENABLED=1
```

Your name, contact details, and links are **not** environment variables — set
them once in the web **Profile** tab and they're stored in the local database.

## Quick start

```bash
# 1. Turn your résumé into structured items (one time)
./ai-resume-tailor decompose my_resume.pdf     # or a .txt file → items.json

# 2. Set your header details (name, contact, links)
./ai-resume-tailor serve                        # open http://127.0.0.1:8080/profile

# 3. Tailor to a posting — paste text, pass a file, or pass a URL
./ai-resume-tailor tailor                       # paste the JD, end with two blank lines
./ai-resume-tailor tailor job.txt
./ai-resume-tailor tailor https://boards.example.com/senior-go-engineer
```

Tailoring writes `Jayce_Park_Company_YYMMDD.pdf` and `.md` to the current
directory and files a draft application with the JD attached. Open the dashboard
to see the draft, change its status, or read the saved posting.

## Commands

| Command | Description |
|---|---|
| `ping` | Check that a provider key works. |
| `decompose [resume.txt\|.pdf]` | Parse your résumé into `items.json`. |
| `match [jd\|url]` | Show how your items score against a posting. |
| `tailor [jd\|url]` | Build a tailored résumé (Markdown + PDF) and file a draft. |
| `prep [jd\|url]` | Generate grounded interview talking points. |
| `track "<company>" "<role>"` | Add an application manually. |
| `apps` | List tracked applications. |
| `status <id> <status>` | Update an application's status. |
| `note <id> <text…>` | Attach a note to an application. |
| `serve [addr]` | Run the web dashboard (default `:8080`). |

For `match`, `tailor`, and `prep`, pass a URL or file path — or pass nothing and
paste the JD, finishing with two blank lines. LinkedIn and other
JavaScript-heavy boards can't be fetched; paste those instead.

Statuses: `draft`, `applied`, `interviewing`, `offer`, `accepted`, `rejected`,
`withdrawn`.

## The web dashboard

`./ai-resume-tailor serve` opens three tabs:

- **Pipeline** — your applications as a board. Change status inline, add notes,
  delete entries, and open the saved JD for any tailored role in a modal.
- **Generate** — paste a JD (or URL) and run tailor or prep as a background job,
  watching the result stream in. Tailor results include a **Download PDF** button.
- **Profile** — your name, contact details, and links, used as the résumé header.

The CLI writes résumé files to disk; the web dashboard offers them as a download
instead, so nothing piles up in the server's directory.

## How "no fabrication" works

The model only ever rewrites bullets that map back to a real item you provided.
After assembly, a verification pass checks every number and claim against the
source; unverifiable figures are reported rather than silently kept. Company
names, dates, and your contact header are filled deterministically from your
items and profile — the model doesn't get to invent them, and education is never
misfiled as work experience.

## Causal tracing

Beyond flagging unverifiable figures, `tailor` can record the *reasoning* behind
the résumé as a causal graph, using
[ai-trace-cause](https://github.com/karosia/ai-trace-cause). Each bullet is
traced along a five-step chain:

```
Source        the résumé item the bullet came from
  │ PRODUCED
Observation   the JD requirement it matched
  │ SUPPORTS
Fact          that the item covers that requirement (confidence = match score)
  │ BASIS_OF
Decision      to include the bullet — or to flag it as unverifiable
  │ CAUSED
Action        placing it in a section, or recording the violation
```

When verification flags a figure, the tool walks this chain backward and prints
a plain-English explanation of why it was flagged — from the action, through the
decision and fact, back to the source item. Confidence values come from the
real match scores, not from the model.

Setting `OTEL_ENABLED=1` wraps each tailor run in an OpenTelemetry span and
correlates every recorded entity with that span's trace and span IDs, so the
"why" (the causal graph) lines up with the "how" (operational tracing). Spans
are printed to stdout; the tool configures the tracer but never sends data
anywhere on its own.

## Project layout

```
ai-resume-tailor/
├── main.go
└── internal/
    ├── cli/         command dispatch, CLI + web wiring, causal-trace + OTel setup
    ├── llm/         provider clients (Anthropic, OpenAI, Gemini) with failover
    ├── jsonx/       tolerant JSON extraction from model output
    ├── resume/      résumé items and decomposition
    ├── jd/          job-description analysis
    ├── jobsource/   resolve a JD from text, a file, or a URL
    ├── tailor/      matching, assembly, company grouping, PDF/Markdown, causal trace
    ├── prep/        interview-prep generation
    ├── jobs/        in-process background job manager
    ├── store/       SQLite: applications, profile
    └── web/         server, handlers, html/template views
```

## Data & privacy

Everything stays on your machine. Your items, profile, applications, and saved
JDs live in a local SQLite file (`ai-resume-tailor.db`); generated résumés are
written to the working directory. The causal graph is in-memory and lives only
for a single tailor run. Nothing is uploaded anywhere except the LLM provider
calls you trigger. Keep `ai-resume-tailor.db`, `items.json`, generated résumé
files, and your `.env` out of version control (see `.gitignore`).

## Troubleshooting

- **Changed a template but the page looks the same.** Templates are embedded
  into the binary at build time (`//go:embed`), so edits require a rebuild and a
  server restart: `go build -o ai-resume-tailor . && ./ai-resume-tailor serve`.
  Browsers cache `localhost` aggressively — use a private window if a hard
  refresh isn't enough.
- **A JD URL comes back empty.** LinkedIn and other JavaScript-rendered or
  bot-protected boards can't be fetched. Paste the description instead.
- **A tailored figure is flagged.** That number couldn't be traced to a source
  item. Fix or remove it before using the résumé — the check is doing its job.
  Run with `OTEL_ENABLED=1` to see the full causal chain behind the flag.

## License

MIT
# AI Resume Tailor

A resume tailoring and job application tracking tool, written in Go.

Point it at your resume and a job description, and it assembles a tailored
resume from your **verified** experience — recombining, rephrasing, and
re-emphasizing facts you've already recorded, without inventing metrics or
claims. It also tracks where you've applied and helps you prepare for
interviews.

> **Status: early development.** The LLM provider layer (Milestone 1) is
> complete and tested. See the [roadmap](#roadmap) for what exists today and
> what's coming.

## Why this exists

General-purpose job trackers don't know your actual accomplishments or your
standards, so auto-generated bullet points tend to inflate or invent. ai-resume-tailor
is built around one hard constraint: the resume assembler may only work from
facts that already exist in your stored resume items. Rewording and reordering
are allowed; fabricating a number or an experience is not. That guardrail is the
core design principle the rest of the project is built to enforce.

## What's implemented today (Milestone 1)

A provider-agnostic LLM client with automatic failover:

- A single `Provider` interface — add a new backend by implementing two methods.
- Ships with an **Anthropic** provider (primary) and an **OpenAI** provider
  (fallback).
- Ordered failover: providers are tried in priority order; the first success is
  returned, and if every provider fails the errors are aggregated into one.
- No third-party dependencies — standard library only.

## Architecture

The guiding rule: nothing outside `internal/llm` imports a specific provider.
The rest of the application depends only on the `Provider` interface and the
`Client`. Adding a provider is one new file plus one line of registration —
no other code changes.

```
ai-resume-tailor/
├── main.go                  # wires providers from env, runs a demo call
└── internal/
    └── llm/
        ├── llm.go           # Provider interface + provider-agnostic types
        ├── client.go        # ordered failover chain
        ├── anthropic.go     # Anthropic Messages API provider
        ├── openai.go        # OpenAI Chat Completions provider
        └── client_test.go   # failover tests (no network required)
```

Each provider translates the shared, provider-agnostic `Request` into its own
wire format, so per-provider quirks (for example, how a system prompt is passed)
stay contained inside that provider's file and never leak into the rest of the
app.

## Requirements

- Go 1.21 or newer (uses `log/slog`).

## Getting started

```bash
# Run the failover tests — no API keys required.
go test ./...

# Make a real call.
export ANTHROPIC_API_KEY=sk-ant-...
export OPENAI_API_KEY=sk-...        # optional; enables fallback
go run .
```

### Configuration

Configuration is read from environment variables:

| Variable            | Required | Default            | Purpose                          |
| ------------------- | -------- | ------------------ | -------------------------------- |
| `ANTHROPIC_API_KEY` | yes\*    | —                  | Enables the Anthropic provider   |
| `ANTHROPIC_MODEL`   | no       | `claude-sonnet-5`  | Override the Anthropic model     |
| `OPENAI_API_KEY`    | no       | —                  | Enables the OpenAI fallback      |
| `OPENAI_MODEL`      | no       | `gpt-4o`           | Override the OpenAI model         |

\* At least one provider key must be set. The primary/fallback order is
determined by the order providers are registered in `main.go` (Anthropic first,
OpenAI second).

## Adding a provider

Implement the `Provider` interface in a new file under `internal/llm/`:

```go
type Provider interface {
    Name() string
    Complete(ctx context.Context, req Request) (*Response, error)
}
```

Then register it in `main.go`:

```go
if url := os.Getenv("OLLAMA_URL"); url != "" {
    providers = append(providers, llm.NewOllamaProvider(url, "llama3"))
}
```

Nothing else needs to change. The failover `Client` and all downstream features
work against the interface, not any concrete provider.

## Roadmap

- [x] **M1** — LLM provider abstraction + ordered failover
- [ ] **M2** — Resume decomposition into structured, reusable items
- [ ] **M3** — JD analysis, item matching, and guardrailed tailored-resume generation
- [ ] **M4** — Application tracking (status, dates, history) backed by SQLite
- [ ] **M5** — Interview prep: expected questions + answer examples grounded in stored items
- [ ] **M6** — Web UI to tie it together

## License

Personal project. Choose and add a license (e.g. MIT) before sharing or reuse.

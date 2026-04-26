# My TU Brain — CLAUDE.md

This file is the authoritative reference for Claude Code when working in this repository.

## What this project is

My TU Brain is a Go CLI study copilot that ingests lecture PDFs and produces structured "First Thoughts" markdown ledgers, a SQLite concept index, and an interactive quiz mode. It is purpose-built for a student in an AI/ML Master's program at TU Darmstadt, where grades depend entirely on a single high-stakes written exam per course.

Core design decisions:
- **Single synthesis pass** — after per-page VLM enrichment, one combined LLM call produces both atomic concept nodes (→ SQLite) and the First Thoughts markdown document (→ `courses/`). This minimises API cost vs. the alternative of separate extraction + synthesis passes.
- **SQLite FTS5 as the only retrieval layer** — no Bleve, no spreading activation. FTS5 is sufficient for the query volume and provides zero operational overhead.
- **Scratchpad protection** — `<!-- my-tu-brain:scratchpad -->` is the hard delimiter in every ledger. On re-ingest, everything above is regenerated; everything below is preserved exactly.
- **No graph edges** — the original design had Hebbian-weighted edges and spreading activation. These were removed as over-engineering for the actual use case (study notes + Q&A).

## Build

```bash
# CGO_ENABLED=1 is required — go-sqlite3 is a CGo library
CGO_ENABLED=1 go build -o ./build/my-tu-brain ./cmd/my-tu-brain

# Or via Make
make build
```

## Run

```bash
my-tu-brain ingest lecture.pdf --course deep_learning --exam-date 2026-06-15
my-tu-brain ask "How does backpropagation use the chain rule?"
my-tu-brain ask "Explain attention" --course deep_learning --limit 15
my-tu-brain search "gradient descent" --limit 5
my-tu-brain quiz --course deep_learning
my-tu-brain quiz --course deep_learning --lecture "Lecture_03"
my-tu-brain status
```

## Test

```bash
go test ./...
```

Tests that need a real LLM must be skipped with `t.Skip` unless `ANTHROPIC_API_KEY` is set.

## Module

```
github.com/truongvinh/my-tu-brain   (Go 1.25.1)
```

## Key dependencies

| Package | Role |
|---------|------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/spf13/viper` | YAML config loading |
| `github.com/mattn/go-sqlite3` | SQLite driver (CGo, includes FTS5) |
| `github.com/ledongthuc/pdf` | Pure Go PDF text extraction |
| `github.com/pdfcpu/pdfcpu` | Pure Go PDF page splitting for VLM input |
| `github.com/rs/zerolog` | Structured logging |
| `github.com/schollz/progressbar/v3` | CLI progress bars |

No Bleve, no tiktoken, no graph packages. The only CGo dependency is `go-sqlite3`.

## Architecture

```
my-tu-brain CLI (Cobra)
        │
        ├── ingest → ingestion.Pipeline
        │               ├── TextExtractor     (ledongthuc/pdf)
        │               ├── PageClassifier    (heuristics: LaTeX / tables / images)
        │               ├── VLMWorker         (fan-out/fan-in; per-page enrichment)
        │               ├── llm.Synthesize()  (ONE call → nodes JSON + First Thoughts md)
        │               ├── storage.DB        (upsert nodes into SQLite)
        │               └── MarkdownWriter    (write/update .md in courses/)
        │
        ├── ask   → search.Search (FTS5)
        │           → llm.Answer (assemble context → LLM)
        │           → storage.LogQuery
        │
        ├── quiz  → read courses/<course>/*.md (strip scratchpad)
        │           → llm.Chat (multi-turn, Reverse-Interrogation prompt)
        │
        ├── search → search.Search (FTS5)
        │
        └── status → storage.DB.Stats

Data layer:
  my-tu-brain.db     — SQLite (nodes, FTS5 virtual table, query_log)
  courses/        — First Thoughts markdown ledgers
```

## Package map

| Package | Path | Responsibility |
|---------|------|---------------|
| `cli` | `internal/cli/` | Cobra command wiring; thin — delegates to internal packages |
| `config` | `internal/config/` | Typed config struct; all defaults live in `setDefaults()` |
| `storage` | `internal/storage/` | SQLite open/migrate, Node CRUD, FTS5, stats, query log |
| `ingestion` | `internal/ingestion/` | Full PDF → synthesis pipeline; text extractor, classifier, VLM pool, markdown writer |
| `search` | `internal/search/` | Thin FTS5 wrapper over `storage.DB.FTSSearch` |
| `llm` | `internal/llm/` | HTTP client for Anthropic / Gemini / OpenAI / Ollama with retry; `Synthesize()`, `Answer()`, `Chat()`, `ExtractPage()` |

## File-by-file

```
internal/cli/root.go              Cobra root, initConfig, logger setup
internal/cli/ingest.go            my-tu-brain ingest — flags, calls Pipeline.Run
internal/cli/ask.go               my-tu-brain ask — FTS5 → context assembly → LLM → print
internal/cli/search.go            my-tu-brain search — FTS5 results, formatted output
internal/cli/status.go            my-tu-brain status — prints DB.Stats
internal/cli/quiz.go              my-tu-brain quiz — multi-turn interactive exam session

internal/config/config.go         Config struct + Load() + setDefaults()

internal/storage/sqlite.go        Open() with WAL + FK pragmas; Stats(); LogQuery()
internal/storage/schema.go        migrate() — DDL: nodes, FTS5, query_log (no edges)
internal/storage/nodes.go         UpsertNode, GetNode, FTSSearch (with course filter), ListNodes

internal/ingestion/pipeline.go    Pipeline.Run() — orchestration
internal/ingestion/text_extractor.go  Pure Go text extraction; ExtractPagePDF() via pdfcpu
internal/ingestion/page_classifier.go IsSimple() heuristics; textToExtractedContent()
internal/ingestion/vlm_worker.go  ProcessPages() fan-out/fan-in; per-page enrichment only
internal/ingestion/markdown_writer.go WriteLecture() — First Thoughts file + scratchpad merge

internal/search/fts.go            Search() — sanitises query, calls db.FTSSearch

internal/llm/client.go            Synthesize() — combined nodes+markdown synthesis
                                  Answer() — Q&A from context
                                  Chat() — multi-turn for quiz (Anthropic native; fallback for others)
                                  Extract() / ExtractPage() — per-page VLM enrichment
                                  doWithRetry() — exponential backoff
```

## SQLite schema

```sql
nodes (id PK, display_name, definition, category, course, exam_date,
       raw_latex JSON, sources JSON, created_at, updated_at)

nodes_fts  — FTS5 virtual table, content='nodes', kept in sync by 3 triggers

query_log (id, query_text, matched_nodes JSON, timestamp)
```

Key index: `idx_nodes_course`. FTS5 handles all full-text retrieval.

## Output directory structure

```
courses/
├── deep_learning/
│   ├── lecture_01_intro.md
│   └── lecture_02_backprop.md
└── probabilistic_models/
    └── lecture_01_foundations.md
```

Course directory names are lowercased and spaces replaced with `_` (see `sanitiseCourse()`). Filenames are the PDF basename slugified via `toSlug()`.

## Scratchpad protection

The delimiter `<!-- my-tu-brain:scratchpad -->` divides every ledger:

- Everything **above** the delimiter is LLM-generated and replaced on re-ingest.
- Everything **below** is the student's personal notes and is **never touched**.

`markdown_writer.go:mergeWithExisting()` implements this split. If the delimiter is absent (first ingest or manually removed), the whole file is overwritten.

## LLM synthesis flow

`llm.Client.Synthesize()` sends one request with:
- System prompt: First Thoughts prompt (baked into `firstThoughtsSystemPrompt()` in `client.go`)
- User message: all page texts combined + metadata (course, exam date, filename)

The LLM returns:
1. A ` + "```" + `json block with an array of `NodeDraft` objects
2. The complete First Thoughts markdown document

`parseSynthesisResponse()` splits on the json block delimiters to extract both parts.

## LLM providers

| Value | API endpoint | Env var | PDF input |
|-------|-------------|---------|-----------|
| `anthropic` | `api.anthropic.com/v1/messages` | `ANTHROPIC_API_KEY` | Native (document block) |
| `gemini` | `generativelanguage.googleapis.com/v1beta/...` | `GEMINI_API_KEY` | Native (inline_data) |
| `openai` | `api.openai.com/v1/chat/completions` | `OPENAI_API_KEY` | Falls back to text |
| `ollama` | `localhost:11434/api/generate` | *(none)* | Falls back to text |

`Chat()` is natively multi-turn for Anthropic and OpenAI. Gemini and Ollama use a text-concatenation fallback.

Default model: `claude-haiku-4-5-20251001`. Change via `llm.model` in `my-tu-brain.yaml`.

## Page classifier heuristics

A page is flagged **complex** (→ VLM) if ANY is true:
- Extracted text length < 50 chars
- Regex matches `\frac`, `\sum`, `\int`, `\begin{equation}`, `$$`, `\[`
- Regex matches `||` patterns or `\begin{tabular}`
- > 60% of non-empty lines are < 60 chars wide (multi-column garble)

## Config file

```yaml
output:
  content_dir: ./courses      # where First Thoughts ledgers are written

database:
  path: ./my-tu-brain.db

llm:
  provider: anthropic
  model: claude-haiku-4-5-20251001
  api_key_env: ANTHROPIC_API_KEY
  max_retries: 3

ingestion:
  workers: 4                  # default: runtime.NumCPU()
```

All fields have defaults in `config.setDefaults()`.

## Common pitfalls

- **Never** run `go build` without `CGO_ENABLED=1` — `go-sqlite3` will fail.
- FTS5 sync triggers fire on SQLite INSERT/UPDATE/DELETE. Do not bypass them with direct `sqlite3` CLI edits or the FTS index will be stale. If it happens, delete `my-tu-brain.db` and re-ingest.
- `Synthesize()` sends all page texts in one request. Very long PDFs (100+ pages) may approach the model's context limit. A future improvement would be to chunk the synthesis into sections.
- The quiz command loads entire lecture files into the system prompt. Use `--lecture` to scope to specific files for large courses.
- `doWithRetry` re-reads the request body from the `body []byte` arg on each retry — do not rely on `req.Body` being replayable after the first attempt.
- For Gemini, the API key is always read from `GEMINI_API_KEY`. OpenAI always reads `OPENAI_API_KEY`. Only `api_key_env` in config controls the Anthropic key lookup.

## Environment

- macOS (darwin/amd64 confirmed)
- `CGO_ENABLED=1` mandatory at build time
- One of `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `OPENAI_API_KEY` must be set at runtime (or Ollama running locally)

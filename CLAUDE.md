# BioGraph — CLAUDE.md

This file is the authoritative reference for Claude Code when working in this repository.

## What this project is

BioGraph is a Go CLI tool that ingests lecture PDFs into an associative knowledge graph and answers academic questions using spreading activation retrieval. It writes all entities as Obsidian markdown notes so the graph is browsable in Obsidian's graph view.

Core ideas driving every design decision:
- **Tiered ingestion** — text-only pages skip the VLM; only complex pages (equations, figures, tables) go to a vision model. Keeps cost low.
- **SQLite as the graph store** — nodes and edges live in SQLite with adjacency indexes; FTS5 provides full-text fallback without a separate search daemon.
- **Bleve for ranking** — BM25/TF-IDF over entity names and definitions. Confidence score gates whether the LLM router runs.
- **Spreading activation** — energy flows outward from starting nodes along weighted edges; intersection nodes (reachable from multiple starts) naturally rank highest.
- **Sigmoid-bounded Hebbian learning** — `w_new = 1 / (1 + exp(-(w_old + α - c)))`. Weights stay strictly in (0, 1) and can never dominate. α=0.05, c=0.5.
- **Exam-aware decay** — edge weights decay at rates that depend on days until the next exam (freeze on exam day, aggressive post-exam cleanup).

## Build

```bash
# CGO_ENABLED=1 is required — go-sqlite3 is a CGo library
CGO_ENABLED=1 go build -o ./build/biograph ./cmd/biograph

# Or via Make
make build
```

The binary is placed at `./build/biograph`.

## Run

```bash
# All commands auto-load biograph.yaml from the current directory
./build/biograph ingest lecture.pdf --course deep_learning --exam-date 2026-06-15
./build/biograph ask "How does backpropagation use the chain rule?"
./build/biograph search "gradient descent" --limit 5
./build/biograph status
```

## Test

```bash
go test ./...
```

No external services are required for tests. Tests that need a real LLM should be skipped with `t.Skip` unless `ANTHROPIC_API_KEY` is set.

## Module

```
github.com/truongvinh/biograph   (Go 1.25.1)
```

## Key dependencies

| Package | Role |
|---------|------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/spf13/viper` | YAML config loading |
| `github.com/mattn/go-sqlite3` | SQLite driver (CGo, includes FTS5) |
| `github.com/blevesearch/bleve/v2` | BM25/TF-IDF full-text search index |
| `github.com/pkoukk/tiktoken-go` | Token counting for context packing |
| `github.com/rs/zerolog` | Structured logging |
| `github.com/schollz/progressbar/v3` | CLI progress bars |

External tools (not Go packages):
- `pdftotext` + `pdfinfo` — from `brew install poppler`. Required for text extraction.
- `ollama` — optional, for local LLM inference.

## Architecture

```
Obsidian Vault (.md files)
        ↑ written by markdown_writer
        │
biograph CLI (Cobra)
        │
        ├── ingest → ingestion.Pipeline
        │               ├── TextExtractor   (pdftotext, page by page)
        │               ├── PageClassifier  (heuristics: LaTeX / tables / images)
        │               ├── VLMWorker       (fan-out/fan-in pool → LLM API)
        │               ├── storage.DB      (SQLite upsert nodes + edges)
        │               ├── search.Index    (Bleve index node)
        │               └── MarkdownWriter  (write/update .md in vault)
        │
        ├── ask   → search.Router (Bleve → LLM fallback → FTS5)
        │           → graph.ActivationEngine (spreading activation)
        │           → graph.ContextPacker    (token-aware assembly)
        │           → llm.Client             (answer generation)
        │           → graph.ActivationEngine.Reinforce (Hebbian update)
        │
        ├── search → search.Index.Search (Bleve BM25)
        │
        └── status → storage.DB.Stats

Data layer:
  biograph.db     — SQLite (nodes, edges, FTS5 virtual table, query_log)
  biograph.bleve  — Bleve index directory
  vault/          — Obsidian markdown notes
```

## Package map

| Package | Path | Responsibility |
|---------|------|---------------|
| `cli` | `internal/cli/` | Cobra command wiring; thin — delegates to internal packages |
| `config` | `internal/config/` | Typed config struct; all defaults live in `setDefaults()` |
| `storage` | `internal/storage/` | SQLite open/migrate, Node/Edge CRUD, FTS5, stats, decay |
| `ingestion` | `internal/ingestion/` | Full PDF → graph pipeline; text extractor, classifier, VLM pool, markdown writer |
| `search` | `internal/search/` | Bleve index wrapper; hybrid router (Bleve → LLM → FTS5) |
| `graph` | `internal/graph/` | Spreading activation, context packer, plasticity manager |
| `llm` | `internal/llm/` | HTTP client for Anthropic / OpenAI / Ollama with retry |

## File-by-file

```
internal/cli/root.go              Cobra root, initConfig, logger setup
internal/cli/ingest.go            `biograph ingest` — flags, validation, calls Pipeline.Run
internal/cli/ask.go               `biograph ask` — router → activation → pack → LLM → reinforce
internal/cli/search.go            `biograph search` — Bleve search, result formatting
internal/cli/status.go            `biograph status` — prints DB.Stats

internal/config/config.go         Config struct + Load() + setDefaults()

internal/storage/sqlite.go        Open() with WAL + FK pragmas; Stats(); LogQuery()
internal/storage/schema.go        migrate() — full DDL (idempotent IF NOT EXISTS)
internal/storage/nodes.go         UpsertNode (merge sources), GetNode, FTSSearch, ListTopNodes
internal/storage/edges.go         UpsertEdge, GetNeighbors (bidirectional), UpdateEdgeWeight,
                                  DecayEdgesForCourse, DecayAllEdges, decayFactor()

internal/ingestion/pipeline.go    Pipeline.Run() — top-level orchestration
internal/ingestion/text_extractor.go  pdftotext page extraction; pageCount via pdfinfo/pdfcpu
internal/ingestion/page_classifier.go IsSimple() heuristics; textToExtractedContent()
internal/ingestion/vlm_worker.go  ProcessPages() fan-out/fan-in; buildExtractionPrompt()
internal/ingestion/markdown_writer.go WriteEntity(), WriteSource(), renderEntityNote()

internal/search/bleve_index.go    OpenIndex (create if missing), IndexNode, Search, TopScore
internal/search/router.go         Route() — Bleve → llmRoute → ftsRoute fallback chain

internal/graph/activation.go      ActivationEngine.Activate() — concurrent hop-by-hop traversal
                                  ActivationEngine.Reinforce() — pairwise Hebbian sigmoid update
internal/graph/context_packer.go  ContextPacker.Pack() — token-budget assembly with tiktoken
internal/graph/plasticity.go      PlasticityManager.RunDecay(); SigmoidUpdate()

internal/llm/client.go            Client.Answer(), Extract(), Route() — dispatches to provider
                                  anthropicComplete / openaiComplete / ollamaComplete
                                  doWithRetry() — exponential backoff
```

## SQLite schema

```sql
nodes (id PK, display_name, definition, category, course, exam_date,
       raw_latex JSON, sources JSON, created_at, updated_at)

edges (source_id FK, target_id FK, weight REAL [0,1], edge_type,
       last_fired, created_at)   PRIMARY KEY (source_id, target_id)

nodes_fts  — FTS5 virtual table, content='nodes', kept in sync by 3 triggers

query_log (id, query_text, activated_nodes JSON, timestamp)
```

Key indexes: `idx_edges_source`, `idx_edges_target`, `idx_edges_weight`, `idx_nodes_course`, `idx_nodes_exam`.

Neighbor fetches are **bidirectional** — the WHERE clause is `source_id = ? OR target_id = ?` with a CASE to always return the other side.

## Hebbian weight update

Applied in `storage.DB.UpdateEdgeWeight` (SQL) and `graph.SigmoidUpdate` (Go):

```
w_new = 1 / (1 + exp(-(w_old + α - c)))
α = 0.05  (reinforcement rate)
c = 0.5   (sigmoid center — baseline weight)
```

Weights asymptote toward 1.0 but never reach it. Change is fastest near 0.5.

## Exam-aware decay schedule

Computed in `storage.decayFactor(daysUntilExam)`:

| Days until exam | Decay factor | Behaviour |
|----------------|-------------|-----------|
| > 30           | 0.999       | Long-term retention |
| 7–30           | 0.9999      | Exam prep — near-zero decay |
| 0–7            | 1.0         | Freeze — exam imminent |
| -7 to 0        | 0.995       | Post-exam cooldown |
| < -7           | 0.98        | Aggressive post-exam cleanup |

## Spreading activation parameters (config defaults)

| Key | Default | Meaning |
|-----|---------|---------|
| `activation.max_hops` | 2 | Traversal depth from starting nodes |
| `activation.min_energy` | 0.05 | Prune threshold — paths below this are cut |
| `activation.decay_per_hop` | 0.7 | Energy multiplier per hop |
| `activation.max_context_nodes` | 20 | Cap on nodes packed into LLM context |
| `activation.max_context_tokens` | 6000 | Token budget for context assembly |

## LLM providers

Controlled by `llm.provider` in `biograph.yaml`:

| Value | API | Env var |
|-------|-----|---------|
| `anthropic` | `api.anthropic.com/v1/messages` | `ANTHROPIC_API_KEY` |
| `openai` | `api.openai.com/v1/chat/completions` | `OPENAI_API_KEY` |
| `ollama` | `localhost:11434/api/generate` | *(none)* |

Default model: `claude-haiku-4-5-20251001`. Change via `llm.model` and `llm.vlm_model`.

## Page classifier heuristics

A page is flagged **complex** (→ VLM) if ANY is true:
- Extracted text length < 50 chars (image-heavy / blank)
- Regex matches `\frac`, `\sum`, `\int`, `\begin{equation}`, `$$`, `\[`
- Regex matches `||` patterns or `\begin{tabular}` (tables)
- > 60% of non-empty lines are < 60 chars wide (multi-column garble)

## Vault structure

```
vault/
├── entities/
│   ├── algorithm/        ← one subdir per category
│   ├── concept/
│   └── theorem/
├── sources/              ← one note per ingested PDF
├── dashboards/
│   └── exam_prep.md      ← Dataview dashboard (requires Dataview plugin)
└── templates/
    └── entity_template.md
```

Entity notes have YAML frontmatter with `id`, `display_name`, `category`, `course`, `exam_date`, `last_updated`, `sources`. Connections section uses `[[wikilinks]]` which populate Obsidian's graph view automatically.

## Config file

`biograph.yaml` is loaded from the current working directory (or `--config` flag). All fields have defaults in `config.setDefaults()` — the only field with no useful default is `vault.path`.

## Environment

- macOS (darwin/amd64 confirmed)
- `CGO_ENABLED=1` is mandatory at build time
- `ANTHROPIC_API_KEY` must be set at runtime (or `OPENAI_API_KEY` / Ollama running)
- `pdftotext` must be on `$PATH` (or set `ingestion.pdftotext_path` to the full path)

## Common pitfalls

- **Never** run `go build` without `CGO_ENABLED=1` — `go-sqlite3` will fail silently on some setups.
- Bleve and SQLite are kept in sync manually: `UpsertNode` writes to SQLite, then `IndexNode` writes to Bleve. If they diverge (e.g. manual DB edit), delete `biograph.bleve` and re-ingest.
- The FTS5 sync triggers fire on SQLite INSERT/UPDATE/DELETE. Do not bypass them with direct `sqlite3` CLI edits, or the FTS index will be stale.
- `GetNeighbors` returns both directions of an edge. Spreading activation therefore naturally propagates in both directions — do not add a separate reverse-traversal pass.
- The LLM router receives the top 50 nodes by average edge weight as candidates. If the graph is empty, it returns an error immediately — ingest at least one PDF before querying.

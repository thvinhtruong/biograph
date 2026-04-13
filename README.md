# BioGraph

A Go CLI tool that builds an associative knowledge graph from lecture PDFs, stored as an Obsidian vault, with spreading-activation retrieval and adaptive Hebbian learning.

## How it works

1. **Ingest** — PDFs are split page-by-page. Text-rich pages are extracted directly via `pdftotext`. Pages with equations, figures, or tables are sent to a VLM (Claude/GPT-4o/Ollama) for structured extraction.
2. **Store** — Entities and relationships are stored in SQLite with FTS5 full-text search. Bleve provides BM25/TF-IDF ranking. Each entity also becomes an Obsidian markdown note.
3. **Ask** — A query is routed to starting nodes via Bleve (falls back to LLM router). Spreading activation traverses the graph outward, accumulating energy along weighted edges. The top-ranked nodes are packed into an LLM context window and answered.
4. **Learn** — Each query reinforces co-activated edges using a sigmoid-bounded Hebbian update. Weights decay at rates that depend on how far away the next exam is.

---

## Prerequisites

### System tools

| Tool | Purpose | Install |
|------|---------|---------|
| Go 1.21+ | Build the CLI | [go.dev/dl](https://go.dev/dl/) |
| GCC / Xcode CLT | Required for `go-sqlite3` (CGo) | `xcode-select --install` |
| poppler | `pdftotext` + `pdfinfo` for text extraction | `brew install poppler` |
| Ollama *(optional)* | Local LLM/VLM inference | `brew install ollama` |

Verify poppler is installed:

```bash
pdftotext -v
pdfinfo -v
```

### LLM provider — pick one

| Provider | Required env var | Notes |
|----------|----------------|-------|
| Anthropic (default) | `ANTHROPIC_API_KEY` | Claude Haiku is fast and cheap for ingestion |
| OpenAI | `OPENAI_API_KEY` | Set `llm.provider: openai` in config |
| Ollama | *(none)* | Set `llm.provider: ollama`, run `ollama serve` first |

Export your key before running any command:

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
```

---

## Installation

```bash
# Clone / navigate to the project
cd /path/to/biograph

# Build the binary (CGo required for SQLite)
CGO_ENABLED=1 go build -o ./build/biograph ./cmd/biograph

# Optional: install system-wide
cp ./build/biograph /usr/local/bin/biograph
```

Or use Make:

```bash
make build
```

---

## Configuration

Copy and edit `biograph.yaml` in the project root (it is auto-loaded from the current directory):

```yaml
vault:
  path: "/Users/you/obsidian/BioGraph"   # ← point to your Obsidian vault folder
  entity_dir: "entities"
  source_dir: "sources"

database:
  path: "./biograph.db"

search:
  bleve_index_path: "./biograph.bleve"
  bleve_confidence_threshold: 0.5
  llm_fallback_enabled: true

activation:
  max_hops: 2
  min_energy: 0.05
  decay_per_hop: 0.7
  max_context_nodes: 20
  max_context_tokens: 6000

plasticity:
  reinforcement_alpha: 0.05
  sigmoid_center: 0.5
  decay_enabled: true
  decay_interval: "24h"

llm:
  provider: "anthropic"             # anthropic | openai | ollama
  model: "claude-haiku-4-5-20251001"
  vlm_model: "claude-haiku-4-5-20251001"
  api_key_env: "ANTHROPIC_API_KEY"
  max_retries: 3

ingestion:
  workers: 8                        # concurrent VLM workers (default: num CPU)
  force_vlm: false                  # true = send every page to VLM
  pdftotext_path: "pdftotext"       # full path if not on $PATH
```

**Minimum required change:** set `vault.path` to wherever your Obsidian vault lives (the directory must already exist).

---

## Usage

### Ingest a lecture PDF

```bash
biograph ingest lecture03.pdf --course deep_learning --exam-date 2026-06-15
```

Flags:

| Flag | Description |
|------|-------------|
| `--course` | Tag for the course (used for filtering and exam decay) |
| `--exam-date` | `YYYY-MM-DD` — drives the exam-aware weight decay schedule |
| `--force-vlm` | Skip the text-first pass, send every page to the VLM |
| `--workers N` | Number of concurrent VLM API calls (default: number of CPUs) |
| `--provider` | Override LLM provider for this run: `anthropic`, `openai`, `ollama` |

After ingestion, markdown notes appear in your Obsidian vault under `entities/` and `sources/`.

### Ask a question

```bash
biograph ask "How does backpropagation use the chain rule?"
```

```bash
# Limit to one course
biograph ask "Explain batch normalization" --course deep_learning

# Increase traversal depth for broader context
biograph ask "What connects attention mechanisms to transformers?" --hops 3
```

The answer includes source references (PDF name + page number).

### Search the graph

```bash
biograph search "gradient descent"

# Filter by course, limit results
biograph search "loss function" --course deep_learning --limit 5
```

### Graph status

```bash
biograph status
```

Shows total nodes, edges, courses, query count, top entities by activation weight, and upcoming exams.

---

## Obsidian setup

1. Open Obsidian and point it at the vault directory you set in `biograph.yaml`.
2. Install the **Dataview** plugin (Settings → Community plugins → Dataview).
3. Open `dashboards/exam_prep.md` to see a live table of your top concepts sorted by activation weight.
4. Use the Graph View (filtered by `#deep_learning` or similar) to explore entity relationships.

Entity notes are written automatically on every `ingest` run. Wikilinks in the "Connections" section populate the Obsidian graph view with zero extra work.

---

## Make targets

```bash
make build                                  # compile binary to ./build/biograph
make ingest PDF=lecture.pdf COURSE=ml       # ingest a PDF
make ingest PDF=lecture.pdf COURSE=ml EXAM=2026-06-15
make ask Q="What is the kernel trick?"     # ask a question
make search TERM="neural network"          # search
make status                                # show graph stats
make test                                  # run go test ./...
make clean                                 # remove build/, biograph.db, biograph.bleve
```

---

## Project structure

```
biograph/
├── cmd/biograph/main.go          — entry point
├── internal/
│   ├── cli/                      — Cobra commands (ingest, ask, search, status)
│   ├── config/config.go          — typed config with defaults
│   ├── storage/
│   │   ├── sqlite.go             — DB open, migrations, stats
│   │   ├── schema.go             — DDL: nodes, edges, FTS5 virtual table, sync triggers
│   │   ├── nodes.go              — Node upsert / get / FTS search
│   │   └── edges.go              — Edge upsert / Hebbian update / exam-aware decay
│   ├── ingestion/
│   │   ├── pipeline.go           — orchestrator: text → classify → VLM → store → index
│   │   ├── text_extractor.go     — pdftotext page-by-page extraction
│   │   ├── page_classifier.go    — heuristics: LaTeX / tables / image-heavy pages
│   │   ├── vlm_worker.go         — fan-out/fan-in VLM worker pool
│   │   └── markdown_writer.go    — writes Obsidian notes (YAML frontmatter + wikilinks)
│   ├── search/
│   │   ├── bleve_index.go        — Bleve BM25 index: build, query, score
│   │   └── router.go             — Bleve → LLM router → FTS5 fallback chain
│   ├── graph/
│   │   ├── activation.go         — spreading activation with concurrent traversal
│   │   ├── context_packer.go     — token-aware context assembly (tiktoken)
│   │   └── plasticity.go         — sigmoid Hebbian updates + exam-aware decay runner
│   └── llm/client.go             — Anthropic / OpenAI / Ollama HTTP client with retry
├── vault/
│   ├── dashboards/exam_prep.md   — Dataview dashboard (exam prep view)
│   └── templates/entity_template.md
├── biograph.yaml                 — configuration file
└── Makefile
```

---

## Troubleshooting

**`cgo: C compiler not found`**
Run `xcode-select --install` to install Apple's command-line tools (required for `go-sqlite3`).

**`pdftotext: command not found`**
Install poppler: `brew install poppler`. Then verify with `which pdftotext`.

**`env var ANTHROPIC_API_KEY not set`**
Export the key in your shell before running: `export ANTHROPIC_API_KEY="sk-ant-..."`.
You can also add it to `~/.zshrc` or `~/.zprofile` to make it permanent.

**Bleve index out of sync after manual DB edits**
Delete `./biograph.bleve` and re-ingest. The index is rebuilt automatically.

**Ollama: connection refused**
Start the Ollama daemon first: `ollama serve`. Then pull a model: `ollama pull llama3`.

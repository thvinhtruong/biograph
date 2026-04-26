# The Dual-Output Pipeline

During the biograph ingest phase, we split the output into two distinct streams:

The Hidden Brain (Machine-Readable): The VLM extracts the atomic concepts,
definitions, and mathematical boundaries, sinking them directly into biograph.db
and the Bleve index. No markdown files are generated for these nodes. They exist
purely to power the biograph ask command.

The Human Ledger (Human-Readable): The VLM does a final pass over the lecture to
generate the "First Thoughts" summary document for you to read.

The Updated Directory Architecture Adding that single layer of nesting for
courses prevents the root from becoming a dumping ground, without creating a
labyrinth.

Plaintext ai-ml-semester/ ├── inbox/ # Drop PDFs here ├── biograph.yaml ├──
biograph.db # The atomic node retrieval index ├── biograph.bleve └── courses/ #
1-level deep organization ├── deep_learning/ │ └── Lecture_01_Intro.md └──
probabilistic_models/ └── Lecture_01_Foundations.md The Safe-Merge Scratchpad To
guarantee your notes survive a re-ingestion, we need a strict delimiter. We can
update internal/ingestion/markdown_writer.go to look for a specific hidden HTML
comment.

The generated markdown will look like this:

Markdown

# Lecture 01: Introduction to Deep Learning

## 1. Executive Intuition

...

## 2. Core Theoretical Concepts

...

## 3. Mathematical Foundations

...

## 4. 📝 Exam Review

...

## 5. Student Scratchpad & Inquiries

_Everything below this line is preserved during re-ingestion._

[Your handwritten notes, problem set ideas, and math derivations go here] The
Re-ingest Logic: When you run biograph ingest on an updated PDF, the
MarkdownWriter opens the existing .md file. It splits the byte array exactly at
``. It discards the top half, generates the new LLM synthesis for the updated
slides, and seamlessly concatenates your saved bottom half back onto it.

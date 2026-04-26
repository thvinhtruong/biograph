package llm

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/truongvinh/biograph/internal/config"
)

// Client is a minimal LLM API abstraction supporting Anthropic, OpenAI, Gemini, and Ollama.
type Client struct {
	cfg        *config.Config
	httpClient *http.Client
}

func NewClient(cfg *config.Config) *Client {
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 180 * time.Second},
	}
}

// NodeDraft is a concept extracted during synthesis, before storage.
type NodeDraft struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Definition  string   `json:"definition"`
	Category    string   `json:"category"`
	RawLatex    []string `json:"raw_latex"`
}

// SynthesisResult is the combined output of one synthesis LLM call.
type SynthesisResult struct {
	Nodes    []NodeDraft
	Markdown string
}

// Synthesize performs the combined "First Thoughts" pass over all extracted page
// content for a single lecture. It returns both atomic nodes (for SQLite) and
// the full First Thoughts markdown document (for the human ledger).
func (c *Client) Synthesize(pageTexts []string, course, examDate, filename string) (*SynthesisResult, error) {
	system := firstThoughtsSystemPrompt()

	combined := strings.Join(pageTexts, "\n\n---\n\n")
	user := fmt.Sprintf(`Course: %s
Exam Date: %s
Source file: %s

<lecture_content>
%s
</lecture_content>

First, output a JSON block with extracted nodes, then output the full First Thoughts markdown document.

The JSON block must appear first, exactly like this:
`+"```json"+`
[
  {
    "id": "snake_case_id",
    "display_name": "Human Readable Name",
    "definition": "Precise academic definition",
    "category": "algorithm|theorem|concept|model|technique",
    "raw_latex": ["\\frac{...}{...}"]
  }
]
`+"```"+`

After the JSON block, output the complete First Thoughts markdown document following the structure in your instructions.`,
		course, examDate, filename, combined)

	raw, err := c.complete(system, user)
	if err != nil {
		return nil, err
	}

	return parseSynthesisResponse(raw), nil
}

// parseSynthesisResponse splits the LLM response into nodes JSON and markdown.
func parseSynthesisResponse(raw string) *SynthesisResult {
	result := &SynthesisResult{}

	// Extract JSON block between ```json and ```
	jsonStart := strings.Index(raw, "```json")
	jsonEnd := -1
	if jsonStart >= 0 {
		jsonEnd = strings.Index(raw[jsonStart+7:], "```")
	}

	if jsonStart >= 0 && jsonEnd >= 0 {
		jsonContent := raw[jsonStart+7 : jsonStart+7+jsonEnd]
		json.Unmarshal([]byte(strings.TrimSpace(jsonContent)), &result.Nodes)
		// Markdown is everything after the closing ```
		afterJSON := raw[jsonStart+7+jsonEnd+3:]
		result.Markdown = strings.TrimSpace(afterJSON)
	} else {
		// No JSON block found — treat entire response as markdown
		result.Markdown = strings.TrimSpace(raw)
	}

	return result
}

// Answer generates a response to a user query given retrieved context nodes.
func (c *Client) Answer(query, context string) (string, error) {
	system := `You are an academic assistant for a master's student at TU Darmstadt.
Answer the question using ONLY the provided context from the student's notes.
Be precise, use formal definitions, and include relevant equations in LaTeX ($$...$$).
If the context is insufficient, say so explicitly rather than guessing.`

	user := fmt.Sprintf("Context from study notes:\n---\n%s\n---\n\nQuestion: %s", context, query)
	return c.complete(system, user)
}

// Extract calls the LLM with a text-based extraction prompt (used for simple pages).
func (c *Client) Extract(prompt string) (string, error) {
	return c.complete("You are a precise JSON-only academic knowledge extractor.", prompt)
}

// ExtractPage sends a single-page PDF (as raw bytes) to a vision model for
// content extraction. Claude and Gemini accept PDF natively; OpenAI/Ollama fall back to text.
func (c *Client) ExtractPage(pageBytes []byte, pageNum int, textPrompt string) (string, error) {
	b64 := base64.StdEncoding.EncodeToString(pageBytes)
	switch c.cfg.LLM.Provider {
	case "anthropic":
		return c.anthropicExtractPDF(b64, pageNum, textPrompt)
	case "gemini":
		return c.geminiExtractPDF(b64, pageNum, textPrompt)
	default:
		return c.Extract(textPrompt)
	}
}

// Chat sends a multi-turn conversation and returns the assistant reply.
// Used by the quiz command for interactive sessions.
func (c *Client) Chat(system string, history []ChatMessage) (string, error) {
	switch c.cfg.LLM.Provider {
	case "anthropic":
		return c.anthropicChat(system, history)
	case "openai":
		return c.openaiChat(system, history)
	case "gemini":
		return c.geminiChatFallback(system, history)
	case "ollama":
		return c.ollamaChatFallback(system, history)
	default:
		return c.anthropicChat(system, history)
	}
}

// ChatMessage is a single turn in a conversation.
type ChatMessage struct {
	Role    string // "user" or "assistant"
	Content string
}

func (c *Client) complete(system, user string) (string, error) {
	switch c.cfg.LLM.Provider {
	case "anthropic":
		return c.anthropicComplete(system, user)
	case "openai":
		return c.openaiComplete(system, user)
	case "gemini":
		return c.geminiComplete(system, user)
	case "ollama":
		return c.ollamaComplete(system, user)
	default:
		return c.anthropicComplete(system, user)
	}
}

// ── Anthropic ────────────────────────────────────────────────────────────────

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string OR []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type   string           `json:"type"`
	Text   string           `json:"text,omitempty"`
	Source *anthropicSource `json:"source,omitempty"`
}

type anthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) anthropicComplete(system, user string) (string, error) {
	req := anthropicRequest{
		Model:     c.model(),
		MaxTokens: 8192,
		System:    system,
		Messages:  []anthropicMessage{{Role: "user", Content: user}},
	}
	return c.anthropicDo(req)
}

func (c *Client) anthropicChat(system string, history []ChatMessage) (string, error) {
	msgs := make([]anthropicMessage, len(history))
	for i, m := range history {
		msgs[i] = anthropicMessage{Role: m.Role, Content: m.Content}
	}
	req := anthropicRequest{
		Model:     c.model(),
		MaxTokens: 4096,
		System:    system,
		Messages:  msgs,
	}
	return c.anthropicDo(req)
}

func (c *Client) anthropicExtractPDF(b64 string, pageNum int, textPrompt string) (string, error) {
	req := anthropicRequest{
		Model:     c.model(),
		MaxTokens: 4096,
		System:    "You are a precise JSON-only academic knowledge extractor.",
		Messages: []anthropicMessage{{
			Role: "user",
			Content: []anthropicContentBlock{
				{
					Type: "document",
					Source: &anthropicSource{
						Type:      "base64",
						MediaType: "application/pdf",
						Data:      b64,
					},
				},
				{
					Type: "text",
					Text: fmt.Sprintf("Focus on page %d of the document above.\n\n%s", pageNum, textPrompt),
				},
			},
		}},
	}
	return c.anthropicDo(req)
}

func (c *Client) anthropicDo(req anthropicRequest) (string, error) {
	apiKey := os.Getenv(c.cfg.LLM.APIKeyEnv)
	if apiKey == "" {
		return "", fmt.Errorf("env var %s not set", c.cfg.LLM.APIKeyEnv)
	}

	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("content-type", "application/json")

	resp, err := c.doWithRetry(httpReq, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var ar anthropicResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return "", fmt.Errorf("parse anthropic response: %w", err)
	}
	if ar.Error != nil {
		return "", fmt.Errorf("anthropic error: %s", ar.Error.Message)
	}
	if len(ar.Content) == 0 {
		return "", fmt.Errorf("empty anthropic response")
	}
	return ar.Content[0].Text, nil
}

// ── Gemini ───────────────────────────────────────────────────────────────────

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"system_instruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role,omitempty"`
}

type geminiPart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *geminiBlob `json:"inline_data,omitempty"`
}

type geminiBlob struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) geminiComplete(system, user string) (string, error) {
	req := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: system}},
		},
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: user}}},
		},
	}
	return c.geminiDo(req)
}

func (c *Client) geminiExtractPDF(b64 string, pageNum int, textPrompt string) (string, error) {
	req := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: "You are a precise JSON-only academic knowledge extractor."}},
		},
		Contents: []geminiContent{{
			Parts: []geminiPart{
				{InlineData: &geminiBlob{MimeType: "application/pdf", Data: b64}},
				{Text: fmt.Sprintf("Focus on page %d of the document above.\n\n%s", pageNum, textPrompt)},
			},
		}},
	}
	return c.geminiDo(req)
}

func (c *Client) geminiChatFallback(system string, history []ChatMessage) (string, error) {
	var sb strings.Builder
	sb.WriteString(system)
	sb.WriteString("\n\n")
	for _, m := range history {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n\n")
	}
	return c.geminiComplete(system, sb.String())
}

func (c *Client) geminiDo(req geminiRequest) (string, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY not set")
	}

	model := c.model()
	if model == "" {
		model = "gemini-2.0-flash"
	}

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model, apiKey,
	)

	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.doWithRetry(httpReq, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var gr geminiResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return "", fmt.Errorf("parse gemini response: %w", err)
	}
	if gr.Error != nil {
		return "", fmt.Errorf("gemini error: %s", gr.Error.Message)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty gemini response")
	}
	return gr.Candidates[0].Content.Parts[0].Text, nil
}

// ── OpenAI ───────────────────────────────────────────────────────────────────

type openaiRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) openaiComplete(system, user string) (string, error) {
	return c.openaiChat(system, []ChatMessage{{Role: "user", Content: user}})
}

func (c *Client) openaiChat(system string, history []ChatMessage) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY not set")
	}

	model := c.model()
	if model == "" {
		model = "gpt-4o-mini"
	}

	msgs := make([]openaiMessage, 0, len(history)+1)
	msgs = append(msgs, openaiMessage{Role: "system", Content: system})
	for _, m := range history {
		msgs = append(msgs, openaiMessage{Role: m.Role, Content: m.Content})
	}

	body, _ := json.Marshal(openaiRequest{Model: model, Messages: msgs})
	httpReq, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.doWithRetry(httpReq, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var or openaiResponse
	if err := json.Unmarshal(raw, &or); err != nil {
		return "", fmt.Errorf("parse openai response: %w", err)
	}
	if or.Error != nil {
		return "", fmt.Errorf("openai error: %s", or.Error.Message)
	}
	if len(or.Choices) == 0 {
		return "", fmt.Errorf("empty openai response")
	}
	return or.Choices[0].Message.Content, nil
}

// ── Ollama ───────────────────────────────────────────────────────────────────

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaResponse struct {
	Response string `json:"response"`
}

func (c *Client) ollamaComplete(system, user string) (string, error) {
	return c.ollamaChatFallback(system, []ChatMessage{{Role: "user", Content: user}})
}

func (c *Client) ollamaChatFallback(system string, history []ChatMessage) (string, error) {
	model := c.model()
	if model == "" {
		model = "llama3"
	}

	var sb strings.Builder
	sb.WriteString(system)
	sb.WriteString("\n\n")
	for _, m := range history {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n\n")
	}

	body, _ := json.Marshal(ollamaRequest{Model: model, Prompt: sb.String(), Stream: false})
	httpReq, err := http.NewRequest("POST", "http://localhost:11434/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.doWithRetry(httpReq, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var or ollamaResponse
	if err := json.Unmarshal(raw, &or); err != nil {
		return "", fmt.Errorf("parse ollama response: %w", err)
	}
	return or.Response, nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func (c *Client) model() string {
	if c.cfg.LLM.Model != "" {
		return c.cfg.LLM.Model
	}
	switch c.cfg.LLM.Provider {
	case "gemini":
		return "gemini-2.0-flash"
	case "openai":
		return "gpt-4o-mini"
	case "ollama":
		return "llama3"
	default:
		return "claude-haiku-4-5-20251001"
	}
}

// doWithRetry executes an HTTP request with exponential backoff.
// body is passed separately so it can be re-read on retry.
func (c *Client) doWithRetry(req *http.Request, body []byte) (*http.Response, error) {
	maxRetries := c.cfg.LLM.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * time.Second)
			req.Body = io.NopCloser(bytes.NewReader(body))
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("all retries failed: %w", lastErr)
}

// firstThoughtsSystemPrompt returns the system prompt for the synthesis pass.
func firstThoughtsSystemPrompt() string {
	return `You are an elite academic AI assistant specializing in Artificial Intelligence,
Machine Learning, and theoretical Computer Science.

Your task is to ingest raw text and visual data from a university lecture slide
or academic paper and synthesize it into a highly structured, rigorous "First
Thoughts" Markdown ledger.

The user is a Master's student at TU Darmstadt. The grading in this program
relies heavily on high-stakes, theoretical written exams. Therefore, your
synthesis MUST prioritize formal definitions, mathematical rigor, algorithmic
complexity, and edge cases over high-level generalizations.

You will output TWO parts in a single response:

PART 1 — a ` + "```json" + ` block containing an array of atomic concept nodes.
Each node: { "id": "snake_case", "display_name": "...", "definition": "...",
"category": "algorithm|theorem|concept|model|technique", "raw_latex": ["..."] }
Extract 5–20 of the most important concepts. Use empty array for raw_latex if none.

PART 2 — the complete First Thoughts Markdown document using this exact structure:

# [Topic / Lecture Title]

## 1. Executive Intuition

Provide a dense, 2-3 sentence summary of the core problem this lecture solves.
What is the gap in previous knowledge that this specific concept addresses?

## 2. Core Theoretical Concepts

Extract the primary algorithms, architectures, or theorems. For each:

- **Formal Definition:** State the concept rigorously.
- **Mechanism:** How does it work? Be concise.
- **Assumptions/Constraints:** Under what conditions does this hold true or fail?

## 3. Mathematical Foundations

Extract all key equations, derivations, and proofs.

- Format all math using LaTeX blocks (e.g., $$ E = mc^2 $$).
- Define every variable used in the equations explicitly.
- If a proof is outlined, summarize the logical steps.

## 4. 📝 Exam Review (High-Yield Extraction)

Identify the most highly testable material. Focus on:

- **Computational Complexity:** Time and space complexities (Big-O).
- **Comparative Analysis:** Why use Method A over Method B?
- **Known Limitations/Failure Modes:** Where does the math break down?

## 5. Student Scratchpad & Inquiries

<!-- biograph:scratchpad -->

> _Space reserved for personal notes, coding implementations, and questions._`
}

package llm

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// Answer generates a response to a user query given retrieved context.
func (c *Client) Answer(query, context string) (string, error) {
	system := `You are an academic assistant for a master's student.
Answer the question using ONLY the provided knowledge graph context.
Be precise, cite concepts by name, and include relevant equations.
If the context doesn't contain enough information, say so clearly.`

	user := fmt.Sprintf("Knowledge Graph Context:\n---\n%s\n---\n\nQuestion: %s", context, query)
	return c.complete(system, user)
}

// Extract calls the LLM with a text-based extraction prompt.
func (c *Client) Extract(prompt string) (string, error) {
	return c.complete("You are a precise JSON-only academic knowledge extractor.", prompt)
}

// ExtractPage sends a single-page PDF (as raw bytes) to a vision model for
// structured entity extraction. Claude and Gemini accept PDF natively.
// OpenAI falls back to the text prompt since it requires images, not PDFs.
func (c *Client) ExtractPage(pageBytes []byte, pageNum int, textPrompt string) (string, error) {
	b64 := base64.StdEncoding.EncodeToString(pageBytes)
	switch c.cfg.LLM.Provider {
	case "anthropic":
		return c.anthropicExtractPDF(b64, pageNum, textPrompt)
	case "gemini":
		return c.geminiExtractPDF(b64, pageNum, textPrompt)
	default:
		// OpenAI and Ollama don't support raw PDF — fall back to text prompt
		return c.Extract(textPrompt)
	}
}

// Route asks the LLM to map a query to graph entity IDs.
func (c *Client) Route(prompt string) (string, error) {
	return c.complete("You are a knowledge graph router. Respond only with a JSON array.", prompt)
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
	Type   string          `json:"type"`
	Text   string          `json:"text,omitempty"`
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
		MaxTokens: 4096,
		System:    system,
		Messages:  []anthropicMessage{{Role: "user", Content: user}},
	}
	return c.anthropicDo(req)
}

// anthropicExtractPDF sends a base64 PDF page as a document content block.
func (c *Client) anthropicExtractPDF(b64 string, pageNum int, textPrompt string) (string, error) {
	req := anthropicRequest{
		Model:     c.vlmModel(),
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
	return c.geminiDo(req, c.model())
}

// geminiExtractPDF sends a base64 PDF page as inline_data with MIME type application/pdf.
func (c *Client) geminiExtractPDF(b64 string, pageNum int, textPrompt string) (string, error) {
	req := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: "You are a precise JSON-only academic knowledge extractor."}},
		},
		Contents: []geminiContent{{
			Parts: []geminiPart{
				{
					InlineData: &geminiBlob{
						MimeType: "application/pdf",
						Data:     b64,
					},
				},
				{
					Text: fmt.Sprintf("Focus on page %d of the document above.\n\n%s", pageNum, textPrompt),
				},
			},
		}},
	}
	return c.geminiDo(req, c.vlmModel())
}

func (c *Client) geminiDo(req geminiRequest, model string) (string, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY not set")
	}

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
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY not set")
	}

	model := c.model()
	if model == "" {
		model = "gpt-4o-mini"
	}

	body, _ := json.Marshal(openaiRequest{
		Model: model,
		Messages: []openaiMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})

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
	model := c.model()
	if model == "" {
		model = "llama3"
	}

	body, _ := json.Marshal(ollamaRequest{
		Model:  model,
		Prompt: system + "\n\n" + user,
		Stream: false,
	})

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

func (c *Client) vlmModel() string {
	if c.cfg.LLM.VLMModel != "" {
		return c.cfg.LLM.VLMModel
	}
	return c.model()
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

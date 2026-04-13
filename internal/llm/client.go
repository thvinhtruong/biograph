package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/truongvinh/biograph/internal/config"
)

// Client is a minimal LLM API abstraction supporting Anthropic, OpenAI, and Ollama.
type Client struct {
	cfg        *config.Config
	httpClient *http.Client
}

func NewClient(cfg *config.Config) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// Answer generates a response to a user query given retrieved context.
func (c *Client) Answer(query, context string) (string, error) {
	systemPrompt := `You are an academic assistant for a master's student.
Answer the question using ONLY the provided knowledge graph context.
Be precise, cite concepts by name, and include relevant equations.
If the context doesn't contain enough information, say so clearly.`

	userPrompt := fmt.Sprintf(`Knowledge Graph Context:
---
%s
---

Question: %s`, context, query)

	return c.complete(systemPrompt, userPrompt)
}

// Extract calls the LLM to extract structured entity/relationship JSON from page text.
func (c *Client) Extract(prompt string) (string, error) {
	return c.complete("You are a precise JSON-only academic knowledge extractor.", prompt)
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
	case "ollama":
		return c.ollamaComplete(system, user)
	default:
		return c.anthropicComplete(system, user)
	}
}

// --- Anthropic ---

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	apiKey := os.Getenv(c.cfg.LLM.APIKeyEnv)
	if apiKey == "" {
		return "", fmt.Errorf("env var %s not set", c.cfg.LLM.APIKeyEnv)
	}

	model := c.cfg.LLM.Model
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}

	body, _ := json.Marshal(anthropicRequest{
		Model:     model,
		MaxTokens: 4096,
		System:    system,
		Messages:  []anthropicMessage{{Role: "user", Content: user}},
	})

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := c.doWithRetry(req)
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

// --- OpenAI ---

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

	model := c.cfg.LLM.Model
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

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doWithRetry(req)
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

// --- Ollama ---

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaResponse struct {
	Response string `json:"response"`
}

func (c *Client) ollamaComplete(system, user string) (string, error) {
	model := c.cfg.LLM.Model
	if model == "" {
		model = "llama3"
	}

	combined := system + "\n\n" + user
	body, _ := json.Marshal(ollamaRequest{
		Model:  model,
		Prompt: combined,
		Stream: false,
	})

	req, err := http.NewRequest("POST", "http://localhost:11434/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doWithRetry(req)
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

// doWithRetry executes an HTTP request with exponential backoff.
func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	maxRetries := c.cfg.LLM.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * time.Second)
			// Clone request body for retry
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

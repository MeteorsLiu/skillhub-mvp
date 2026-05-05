package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type anthropicConfig struct {
	apiKey string
	model  string
}

func newAnthropicConfig() *anthropicConfig {
	return &anthropicConfig{
		apiKey: os.Getenv("ANTHROPIC_API_KEY"),
		model:  getEnvDefault("ANTHROPIC_MODEL", "claude-sonnet-4-20250514"),
	}
}

func (c *anthropicConfig) valid() bool {
	return c.apiKey != "" && c.model != ""
}

type anthropicReq struct {
	Model     string         `json:"model"`
	Messages  []anthropicMsg `json:"messages"`
	MaxTokens int            `json:"max_tokens"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicContent struct {
	Text string `json:"text"`
}

type anthropicResp struct {
	Content []anthropicContent `json:"content"`
}

type AnthropicReviewer struct {
	cfg    *anthropicConfig
	client *http.Client
}

func NewAnthropicReviewer() *AnthropicReviewer {
	return &AnthropicReviewer{
		cfg:    newAnthropicConfig(),
		client: &http.Client{},
	}
}

func (r *AnthropicReviewer) Review(ctx context.Context, skill SkillSummary, body string) (*ReviewResult, error) {
	if !r.cfg.valid() {
		return &ReviewResult{Passed: true, Reason: "Anthropic LLM not configured"}, nil
	}

	prompt := fmt.Sprintf(
		`You are a security reviewer for AI agent skills. Review the following skill for any malicious or dangerous content.

Skill: %s (%s)
Description: %s

Content:
%s

Reply with exactly one line starting with PASS or REJECT followed by a brief reason.`,
		skill.ID, skill.Version, skill.Description, body,
	)

	reqBody := anthropicReq{
		Model:     r.cfg.model,
		MaxTokens: 256,
		Messages:  []anthropicMsg{{Role: "user", Content: prompt}},
	}
	data, _ := json.Marshal(reqBody)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(data))
	if err != nil {
		return &ReviewResult{Passed: true, Reason: fmt.Sprintf("Anthropic request error: %v", err)}, nil
	}
	httpReq.Header.Set("x-api-key", r.cfg.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return &ReviewResult{Passed: true, Reason: fmt.Sprintf("Anthropic API error: %v", err)}, nil
	}
	defer resp.Body.Close()

	var apiResp anthropicResp
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return &ReviewResult{Passed: true, Reason: "Anthropic response parse error"}, nil
	}

	var reply string
	for _, c := range apiResp.Content {
		reply += c.Text
	}
	reply = strings.TrimSpace(reply)
	if strings.HasPrefix(reply, "REJECT") {
		reason := strings.TrimSpace(reply[6:])
		return &ReviewResult{Passed: false, Reason: reason}, nil
	}
	return &ReviewResult{Passed: true, Reason: "approved"}, nil
}
